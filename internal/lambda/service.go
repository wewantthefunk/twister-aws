package lambda

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/christian/twister/internal/awsserver"
	"github.com/christian/twister/internal/s3buckets"
	"github.com/christian/twister/internal/sqs"
)

// Service implements awsserver.Service for the Lambda JSON API (X-Amz-Target Lambda_20150331.*).
type Service struct {
	Reg     *Registry
	Events  *EventSourceStore
	Invoker *Invoker
}

// NewService wires registry, event source store, and invoker.
func NewService(registryRoot string) *Service {
	return &Service{
		Reg:     NewRegistry(registryRoot),
		Events:  NewEventSourceStore(registryRoot),
		Invoker: &Invoker{},
	}
}

// ServiceName implements awsserver.Service.
func (s *Service) ServiceName() string { return "lambda" }

// Handle implements awsserver.Service (op is e.g. "Invoke", "CreateFunction").
func (s *Service) Handle(w http.ResponseWriter, r *http.Request, op string, body []byte) {
	if s == nil {
		http.Error(w, "not configured", http.StatusInternalServerError)
		return
	}
	region := awsserver.RegionFromContext(r.Context())
	switch op {
	case "CreateFunction":
		s.handleCreateFunction(w, body, region)
	case "Invoke":
		s.handleInvoke(w, body, region)
	case "GetFunction":
		s.handleGetFunction(w, body)
	case "DeleteFunction":
		s.handleDeleteFunction(w, body)
	case "ListFunctions":
		s.handleListFunctions(w, r, region)
	case "CreateEventSourceMapping":
		s.handleCreateEventSourceMapping(w, body, region)
	case "DeleteEventSourceMapping":
		s.handleDeleteEventSourceMapping(w, r, body)
	case "ListEventSourceMappings":
		s.handleListEventSourceMappings(w, body)
	default:
		awsserver.WriteJSON(w, http.StatusBadRequest, awsserver.ErrorResponse{
			Type:    "InvalidParameterValueException",
			Message: "unknown operation: " + op,
		})
	}
}

// OnSQSMessages is registered as sqs.Manager.DequeueHook; runs after non-peek receives.
func (s *Service) OnSQSMessages(region, queueName string, msgs []sqs.Message) {
	if s == nil || s.Invoker == nil || s.Events == nil || s.Reg == nil || len(msgs) == 0 {
		return
	}
	fn, err := s.Events.FindFunctionForSQS(region, queueName)
	if err != nil || fn == "" {
		return
	}
	ev, err := buildSQSEventForLambda(region, queueName, msgs)
	if err != nil {
		return
	}
	_, _, _ = s.InvokeWithPayload(context.Background(), fn, region, ev)
}

// InvokeWithPayload runs the container with JSON payload bytes (used from HTTP and SQS).
func (s *Service) InvokeWithPayload(ctx context.Context, functionName, region string, payload []byte) ([]byte, int, error) {
	cfg, err := s.Reg.Get(functionName)
	if err != nil {
		return nil, 0, err
	}
	if cfg == nil {
		return nil, 0, fmt.Errorf("lambda: function not found: %s", functionName)
	}
	if !DockerCLIAvailable() {
		return nil, 0, fmt.Errorf("lambda: docker CLI not in PATH")
	}
	env := map[string]string{
		"AWS_LAMBDA_FUNCTION_NAME":       cfg.FunctionName,
		"AWS_LAMBDA_FUNCTION_MEMORY_SIZE": fmt.Sprintf("%d", cfg.MemorySize),
		"AWS_DEFAULT_REGION":              s3buckets.NormalizeRegion(region),
		"AWS_REGION":                      s3buckets.NormalizeRegion(region),
		"LAMBDA_TASK_ROOT":                "/var/task",
	}
	if cfg.Handler != "" {
		env["_HANDLER"] = cfg.Handler
	}
	rid := makeRequestID()
	env["AWS_REQUEST_ID"] = rid
	tmo := cfg.Timeout
	mem := cfg.MemorySize
	res := s.Invoker.Run(ctx, cfg.ImageURI, payload, env, mem, tmo)
	if res.Err != nil && res.ExitCode == -1 {
		// e.g. docker not running, OOM killer
		return res.Stdout, 502, res.Err
	}
	// Unhandled function error if exit != 0
	if res.ExitCode != 0 {
		return res.Stdout, 200, &invokeErr{code: "Unhandled", exit: res.ExitCode, stderr: string(res.Stderr)}
	}
	return res.Stdout, 200, nil
}

type invokeErr struct {
	code, stderr string
	exit         int
}

func (e *invokeErr) Error() string { return e.code }

func makeRequestID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// --- CreateFunction ---

type createFunctionReq struct {
	FunctionName *string         `json:"FunctionName"`
	Code         *codeLocation   `json:"Code"`
	PackageType  *string         `json:"PackageType"`
	Handler      *string         `json:"Handler"`
	Role         *string         `json:"Role"`
	Timeout      *int            `json:"Timeout"`
	MemorySize   *int            `json:"MemorySize"`
	Architectures []string        `json:"Architectures"`
}

type codeLocation struct {
	ImageUri *string `json:"ImageUri"`
}

type createFunctionResp struct {
	FunctionName         string   `json:"FunctionName"`
	FunctionArn          string   `json:"FunctionArn"`
	Role                 *string  `json:"Role,omitempty"`
	CodeSize             *int64   `json:"CodeSize,omitempty"`
	MemorySize           *int     `json:"MemorySize,omitempty"`
	Timeout              *int     `json:"Timeout,omitempty"`
	Handler              *string  `json:"Handler,omitempty"`
	PackageType          string   `json:"PackageType"`
	Architectures        []string `json:"Architectures,omitempty"`
	State                string   `json:"State"`
	LastUpdateStatus     string   `json:"LastUpdateStatus"`
}

func (s *Service) handleCreateFunction(w http.ResponseWriter, body []byte, region string) {
	var in createFunctionReq
	if err := json.Unmarshal(body, &in); err != nil {
		awsserver.WriteJSON(w, http.StatusBadRequest, awsserver.ErrorResponse{Type: "InvalidParameterValueException", Message: err.Error()})
		return
	}
	if in.FunctionName == nil || *in.FunctionName == "" {
		awsserver.WriteJSON(w, http.StatusBadRequest, awsserver.ErrorResponse{Type: "InvalidParameterValueException", Message: "FunctionName is required"})
		return
	}
	if in.Code == nil || in.Code.ImageUri == nil || strings.TrimSpace(*in.Code.ImageUri) == "" {
		awsserver.WriteJSON(w, http.StatusBadRequest, awsserver.ErrorResponse{Type: "InvalidParameterValueException", Message: "Code.ImageUri is required (v1: container image only)"})
		return
	}
	cfg := &FunctionConfig{
		FunctionName:  *in.FunctionName,
		Region:        s3buckets.NormalizeRegion(region),
		ImageURI:      strings.TrimSpace(*in.Code.ImageUri),
		Handler:       derefString(in.Handler),
		Timeout:       derefInt(in.Timeout, 30),
		MemorySize:    derefInt(in.MemorySize, 128),
		PackageType:   "Image",
		Role:          derefString(in.Role),
		Architectures: in.Architectures,
	}
	if in.PackageType != nil && *in.PackageType != "" {
		cfg.PackageType = *in.PackageType
	}
	if err := s.Reg.Put(cfg); err != nil {
		awsserver.WriteJSON(w, http.StatusInternalServerError, awsserver.ErrorResponse{Type: "ServiceException", Message: err.Error()})
		return
	}
	arn := functionArnFor(region, cfg.FunctionName)
	awsserver.WriteJSON(w, http.StatusOK, createFunctionResp{
		FunctionName:     cfg.FunctionName,
		FunctionArn:      arn,
		Role:              in.Role,
		MemorySize:        &cfg.MemorySize,
		Timeout:           &cfg.Timeout,
		Handler:           in.Handler,
		PackageType:       cfg.PackageType,
		Architectures:     cfg.Architectures,
		State:              "Active",
		LastUpdateStatus:   "Successful",
	})
}

func functionArnFor(region, name string) string {
	return "arn:aws:lambda:" + s3buckets.NormalizeRegion(region) + ":000000000000:function:" + name
}

func derefString(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

func derefInt(p *int, def int) int {
	if p == nil {
		return def
	}
	return *p
}

// --- GetFunction / Delete / List ---

type nameOnly struct {
	FunctionName *string `json:"FunctionName"`
}

type getFunctionResp struct {
	Configuration FunctionConfig `json:"Configuration"`
	Code          *struct {
		ResolvedImageUri *string `json:"ResolvedImageUri"`
	} `json:"Code,omitempty"`
}

func (s *Service) handleGetFunction(w http.ResponseWriter, body []byte) {
	var in nameOnly
	_ = json.Unmarshal(body, &in)
	if in.FunctionName == nil || *in.FunctionName == "" {
		awsserver.WriteJSON(w, http.StatusBadRequest, awsserver.ErrorResponse{Type: "InvalidParameterValueException", Message: "FunctionName is required"})
		return
	}
	cfg, err := s.Reg.Get(*in.FunctionName)
	if err != nil {
		awsserver.WriteJSON(w, http.StatusInternalServerError, awsserver.ErrorResponse{Type: "ServiceException", Message: err.Error()})
		return
	}
	if cfg == nil {
		awsserver.WriteJSON(w, http.StatusNotFound, awsserver.ErrorResponse{Type: "ResourceNotFoundException", Message: "Function not found: " + *in.FunctionName})
		return
	}
	uri := cfg.ImageURI
	awsserver.WriteJSON(w, http.StatusOK, getFunctionResp{
		Configuration: *cfg,
		Code: &struct {
			ResolvedImageUri *string `json:"ResolvedImageUri"`
		}{ResolvedImageUri: &uri},
	})
}

func (s *Service) handleDeleteFunction(w http.ResponseWriter, body []byte) {
	var in nameOnly
	_ = json.Unmarshal(body, &in)
	if in.FunctionName == nil || *in.FunctionName == "" {
		awsserver.WriteJSON(w, http.StatusBadRequest, awsserver.ErrorResponse{Type: "InvalidParameterValueException", Message: "FunctionName is required"})
		return
	}
	if err := s.Reg.Delete(*in.FunctionName); err != nil {
		awsserver.WriteJSON(w, http.StatusInternalServerError, awsserver.ErrorResponse{Type: "ServiceException", Message: err.Error()})
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type listFuncResp struct {
	Functions []functionSummary `json:"Functions"`
}

type functionSummary struct {
	FunctionName      string  `json:"FunctionName"`
	FunctionArn        string  `json:"FunctionArn"`
	PackageType        string  `json:"PackageType"`
	MemorySize         *int   `json:"MemorySize"`
	Timeout            *int   `json:"Timeout"`
	Architectures      []string `json:"Architectures,omitempty"`
}

func (s *Service) handleListFunctions(w http.ResponseWriter, r *http.Request, signRegion string) {
	names, err := s.Reg.List()
	if err != nil {
		awsserver.WriteJSON(w, http.StatusInternalServerError, awsserver.ErrorResponse{Type: "ServiceException", Message: err.Error()})
		return
	}
	region := awsserver.RegionFromContext(r.Context())
	if region == "" {
		region = signRegion
	}
	var out []functionSummary
	for _, n := range names {
		c, err := s.Reg.Get(n)
		if err != nil || c == nil {
			continue
		}
		rg := c.Region
		if rg == "" {
			rg = region
		}
		m := c.MemorySize
		t := c.Timeout
		out = append(out, functionSummary{
			FunctionName: c.FunctionName,
			FunctionArn:  functionArnFor(rg, c.FunctionName),
			PackageType:  c.PackageType,
			MemorySize:   &m,
			Timeout:      &t,
			Architectures: c.Architectures,
		})
	}
	awsserver.WriteJSON(w, http.StatusOK, listFuncResp{Functions: out})
}

// --- Invoke ---

type invokeRequest struct {
	FunctionName  *string          `json:"FunctionName"`
	Payload       *json.RawMessage `json:"Payload"`
	InvocationType *string         `json:"InvocationType"`
}

type invokeResponse struct {
	StatusCode      int    `json:"StatusCode"`
	FunctionError   string `json:"FunctionError,omitempty"`
	ExecutedVersion string `json:"ExecutedVersion"`
	Payload         string `json:"Payload"`
	LogResult       string `json:"LogResult,omitempty"`
}

func (s *Service) handleInvoke(w http.ResponseWriter, body []byte, region string) {
	var in invokeRequest
	if err := json.Unmarshal(body, &in); err != nil {
		awsserver.WriteJSON(w, http.StatusBadRequest, awsserver.ErrorResponse{Type: "InvalidRequestContentException", Message: err.Error()})
		return
	}
	if in.FunctionName == nil || *in.FunctionName == "" {
		awsserver.WriteJSON(w, http.StatusBadRequest, awsserver.ErrorResponse{Type: "InvalidParameterValueException", Message: "FunctionName is required"})
		return
	}
	payload := []byte("{}")
	if in.Payload != nil && len(*in.Payload) > 0 {
		payload = *in.Payload
	}
	if !DockerCLIAvailable() {
		awsserver.WriteJSON(w, http.StatusInternalServerError, awsserver.ErrorResponse{Type: "ServiceException", Message: "lambda: docker CLI not in PATH"})
		return
	}
	out, status, err := s.InvokeWithPayload(context.Background(), *in.FunctionName, region, payload)
	if err != nil {
		if strings.Contains(err.Error(), "function not found") {
			awsserver.WriteJSON(w, http.StatusNotFound, awsserver.ErrorResponse{Type: "ResourceNotFoundException", Message: err.Error()})
			return
		}
		if status == 502 {
			awsserver.WriteJSON(w, http.StatusBadGateway, awsserver.ErrorResponse{Type: "ServiceException", Message: err.Error()})
			return
		}
	}
	inv := invokeResponse{ExecutedVersion: "$LATEST", StatusCode: 200, Payload: base64.StdEncoding.EncodeToString(out)}
	if err != nil {
		if e, ok := err.(*invokeErr); ok {
			inv.FunctionError = e.code
		} else {
			inv.FunctionError = "Unhandled"
		}
	}
	awsserver.WriteJSON(w, http.StatusOK, inv)
}

// --- Event source (SQS) ---

type createESMReq struct {
	EventSourceArn  *string `json:"EventSourceArn"`
	FunctionName    *string `json:"FunctionName"`
	BatchSize       *int    `json:"BatchSize"`
	Enabled         *bool   `json:"Enabled"`
}

func (s *Service) handleCreateEventSourceMapping(w http.ResponseWriter, body []byte, region string) {
	var in createESMReq
	if err := json.Unmarshal(body, &in); err != nil {
		awsserver.WriteJSON(w, http.StatusBadRequest, awsserver.ErrorResponse{Type: "InvalidParameterValueException", Message: err.Error()})
		return
	}
	if in.EventSourceArn == nil || in.FunctionName == nil {
		awsserver.WriteJSON(w, http.StatusBadRequest, awsserver.ErrorResponse{Type: "InvalidParameterValueException", Message: "EventSourceArn and FunctionName are required"})
		return
	}
	arn := strings.TrimSpace(*in.EventSourceArn)
	qr, _, err := parseSQSResourceFromARN(arn)
	if err != nil {
		awsserver.WriteJSON(w, http.StatusBadRequest, awsserver.ErrorResponse{Type: "InvalidParameterValueException", Message: "EventSourceArn must be an SQS queue ARN: " + err.Error()})
		return
	}
	_ = qr   // queue’s region in the ARN (should match ReceiveMessage’s region for v1)
	_ = region
	if fn, err := s.Reg.Get(*in.FunctionName); err != nil {
		awsserver.WriteJSON(w, http.StatusInternalServerError, awsserver.ErrorResponse{Type: "ServiceException", Message: err.Error()})
		return
	} else if fn == nil {
		awsserver.WriteJSON(w, http.StatusNotFound, awsserver.ErrorResponse{Type: "ResourceNotFoundException", Message: "function not found"})
		return
	}
	u := newUUIDv4()
	st := "Enabled"
	if in.Enabled != nil && !*in.Enabled {
		st = "Disabled"
	}
	bs := 1
	if in.BatchSize != nil && *in.BatchSize > 0 {
		bs = *in.BatchSize
	}
	m := EventSourceMapping{UUID: u, EventSourceArn: arn, FunctionName: *in.FunctionName, State: st, BatchSize: bs}
	if err := s.Events.AddEventSourceMapping(m); err != nil {
		awsserver.WriteJSON(w, http.StatusBadRequest, awsserver.ErrorResponse{Type: "ResourceConflictException", Message: err.Error()})
		return
	}
	awsserver.WriteJSON(w, http.StatusOK, m)
}

func parseSQSResourceFromARN(arn string) (region, queueName string, err error) {
	// arn:aws:sqs:region:account:queuename
	arn = strings.TrimSpace(arn)
	const p = "arn:aws:sqs:"
	if !strings.HasPrefix(arn, p) {
		return "", "", fmt.Errorf("not an SQS arn")
	}
	rest := strings.TrimPrefix(arn, p)
	parts := strings.SplitN(rest, ":", 3)
	if len(parts) < 3 {
		return "", "", fmt.Errorf("invalid SQS arn")
	}
	return parts[0], parts[2], nil
}

func newUUIDv4() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

type delESMReq struct {
	UUID *string `json:"UUID"`
}

func (s *Service) handleDeleteEventSourceMapping(w http.ResponseWriter, r *http.Request, body []byte) {
	var in delESMReq
	_ = json.Unmarshal(body, &in)
	uuid := ""
	if in.UUID != nil {
		uuid = strings.TrimSpace(*in.UUID)
	}
	if uuid == "" {
		uuid = strings.TrimSpace(r.URL.Query().Get("UUID"))
	}
	if uuid == "" {
		awsserver.WriteJSON(w, http.StatusBadRequest, awsserver.ErrorResponse{Type: "InvalidParameterValueException", Message: "UUID is required"})
		return
	}
	if err := s.Events.DeleteByUUID(uuid); err != nil {
		awsserver.WriteJSON(w, http.StatusNotFound, awsserver.ErrorResponse{Type: "ResourceNotFoundException", Message: err.Error()})
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type listESMResp struct {
	EventSourceMappings []EventSourceMapping `json:"EventSourceMappings"`
}

func (s *Service) handleListEventSourceMappings(w http.ResponseWriter, _ []byte) {
	list, err := s.Events.ListEventSourceMappings()
	if err != nil {
		awsserver.WriteJSON(w, http.StatusInternalServerError, awsserver.ErrorResponse{Type: "ServiceException", Message: err.Error()})
		return
	}
	awsserver.WriteJSON(w, http.StatusOK, listESMResp{EventSourceMappings: list})
}

func buildSQSEventForLambda(region, queueName string, msgs []sqs.Message) ([]byte, error) {
	arn := "arn:aws:sqs:" + s3buckets.NormalizeRegion(region) + ":000000000000:" + queueName
	type rec struct {
		MessageId      string `json:"messageId"`
		ReceiptHandle  string `json:"receiptHandle"`
		Body           string `json:"body"`
		AwsRegion      string `json:"awsRegion"`
		EventSourceARN string `json:"eventSourceARN"`
		EventSource    string `json:"eventSource"`
	}
	region = s3buckets.NormalizeRegion(region)
	var recs []rec
	for _, m := range msgs {
		recs = append(recs, rec{
			MessageId:     m.MessageID,
			ReceiptHandle: m.ReceiptHandle,
			Body:          m.Body,
			AwsRegion:     region,
			EventSourceARN: arn,
			EventSource:   "aws:sqs",
		})
	}
	ev := struct {
		Records []rec `json:"Records"`
	}{Records: recs}
	return json.Marshal(ev)
}
