package ec2

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"sort"
	"strings"
	"time"

	"encoding/xml"
)

var errDupKeyPair = errors.New("ec2: duplicate key pair")
var errNotFoundSG = errors.New("ec2: security group not found")

func (s *Service) createKeyPair(w http.ResponseWriter, q url.Values, region, rid string) {
	name := strings.TrimSpace(q.Get("KeyName"))
	if name == "" {
		writeAPIError(w, http.StatusBadRequest, "MissingParameter", "KeyName is required", rid)
		return
	}
	key := kpKey(region, name)
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "InternalError", err.Error(), rid)
		return
	}
	fp, err := rsaFingerprintMD5(&priv.PublicKey)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "InternalError", err.Error(), rid)
		return
	}
	block := &pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(priv)}
	pemOut := string(pem.EncodeToMemory(block))
	kid := newResourceID("key-")

	err = s.store.Update(func(st *diskState) error {
		if _, ok := st.KeyPairs[key]; ok {
			return errDupKeyPair
		}
		st.KeyPairs[key] = keyPairRec{
			Region: region, Name: name, KeyPairID: kid, Fingerprint: fp,
		}
		return nil
	})
	if err != nil {
		if errors.Is(err, errDupKeyPair) {
			writeAPIError(w, http.StatusBadRequest, "InvalidKeyPair.Duplicate", "The keypair already exists", rid)
			return
		}
		writeAPIError(w, http.StatusInternalServerError, "InternalError", err.Error(), rid)
		return
	}

	writeXML(w, http.StatusOK, rid, createKeyPairResponse{
		Xmlns:          ec2XMLNS,
		RequestId:      rid,
		KeyName:        name,
		KeyFingerprint: fp,
		KeyMaterial:    pemOut,
		KeyPairId:      kid,
	})
}

type createVPCResponse struct {
	XMLName   xml.Name `xml:"CreateVpcResponse"`
	Xmlns     string   `xml:"xmlns,attr"`
	RequestId string   `xml:"requestId"`
	VPC       vpcXML   `xml:"vpc"`
}

type vpcXML struct {
	VpcId         string `xml:"vpcId"`
	State         string `xml:"state"`
	CidrBlock     string `xml:"cidrBlock"`
	IsDefault     bool   `xml:"isDefault"`
	DhcpOptionsId string `xml:"dhcpOptionsId"`
}

func (s *Service) createVPC(w http.ResponseWriter, q url.Values, region, rid string) {
	cidr := strings.TrimSpace(q.Get("CidrBlock"))
	if cidr == "" {
		cidr = "10.0.0.0/16"
	}
	id := newResourceID("vpc-")
	if err := s.store.Update(func(st *diskState) error {
		st.VPCs[id] = vpcRec{ID: id, Region: region, CidrBlock: cidr}
		return nil
	}); err != nil {
		writeAPIError(w, http.StatusInternalServerError, "InternalError", err.Error(), rid)
		return
	}
	writeXML(w, http.StatusOK, rid, createVPCResponse{
		Xmlns: ec2XMLNS, RequestId: rid,
		VPC: vpcXML{VpcId: id, State: "available", CidrBlock: cidr, IsDefault: false, DhcpOptionsId: "default"},
	})
}

type createSubnetResponse struct {
	XMLName   xml.Name  `xml:"CreateSubnetResponse"`
	Xmlns     string    `xml:"xmlns,attr"`
	RequestId string    `xml:"requestId"`
	Subnet    subnetXML `xml:"subnet"`
}

type subnetXML struct {
	SubnetId         string `xml:"subnetId"`
	VpcId            string `xml:"vpcId"`
	CidrBlock        string `xml:"cidrBlock"`
	AvailableIpCount int    `xml:"availableIpAddressCount"`
	State            string `xml:"state"`
}

func (s *Service) createSubnet(w http.ResponseWriter, q url.Values, region, rid string) {
	vpcID := strings.TrimSpace(q.Get("VpcId"))
	cidr := strings.TrimSpace(q.Get("CidrBlock"))
	if vpcID == "" || cidr == "" {
		writeAPIError(w, http.StatusBadRequest, "MissingParameter", "VpcId and CidrBlock are required", rid)
		return
	}
	st := s.store.Snapshot()
	vpc, ok := st.VPCs[vpcID]
	if !ok || vpc.Region != region {
		writeAPIError(w, http.StatusBadRequest, "InvalidVpcID.NotFound", fmt.Sprintf("The vpc ID %q does not exist", vpcID), rid)
		return
	}
	sid := newResourceID("subnet-")
	if err := s.store.Update(func(st *diskState) error {
		if _, vok := st.VPCs[vpcID]; !vok {
			return fmt.Errorf("vpc gone")
		}
		st.Subnets[sid] = subnetRec{ID: sid, Region: region, VpcID: vpcID, CidrBlock: cidr}
		return nil
	}); err != nil {
		writeAPIError(w, http.StatusInternalServerError, "InternalError", err.Error(), rid)
		return
	}
	writeXML(w, http.StatusOK, rid, createSubnetResponse{
		Xmlns: ec2XMLNS, RequestId: rid,
		Subnet: subnetXML{
			SubnetId: sid, VpcId: vpcID, CidrBlock: cidr, AvailableIpCount: 251, State: "available",
		},
	})
}

type describeVPCsResponse struct {
	XMLName   xml.Name  `xml:"DescribeVpcsResponse"`
	Xmlns     string    `xml:"xmlns,attr"`
	RequestId string    `xml:"requestId"`
	VpcSet    vpcSetXML `xml:"vpcSet"`
}

type vpcSetXML struct {
	Items []vpcXML `xml:"item"`
}

func (s *Service) describeVPCs(w http.ResponseWriter, q url.Values, region, rid string) {
	want := listIndexed(q, "VpcId")
	st := s.store.Snapshot()
	var items []vpcXML
	for _, v := range st.VPCs {
		if v.Region != region {
			continue
		}
		if len(want) > 0 && !containsFold(want, v.ID) {
			continue
		}
		items = append(items, vpcXML{
			VpcId: v.ID, State: "available", CidrBlock: v.CidrBlock, IsDefault: false, DhcpOptionsId: "default",
		})
	}
	sort.Slice(items, func(i, j int) bool { return items[i].VpcId < items[j].VpcId })
	writeXML(w, http.StatusOK, rid, describeVPCsResponse{Xmlns: ec2XMLNS, RequestId: rid, VpcSet: vpcSetXML{Items: items}})
}

type describeSubnetsResponse struct {
	XMLName   xml.Name     `xml:"DescribeSubnetsResponse"`
	Xmlns     string       `xml:"xmlns,attr"`
	RequestId string       `xml:"requestId"`
	SubnetSet subnetSetXML `xml:"subnetSet"`
}

type subnetSetXML struct {
	Items []subnetXML `xml:"item"`
}

func (s *Service) describeSubnets(w http.ResponseWriter, q url.Values, region, rid string) {
	want := listIndexed(q, "SubnetId")
	st := s.store.Snapshot()
	var items []subnetXML
	for _, sb := range st.Subnets {
		if sb.Region != region {
			continue
		}
		if len(want) > 0 && !containsFold(want, sb.ID) {
			continue
		}
		items = append(items, subnetXML{
			SubnetId: sb.ID, VpcId: sb.VpcID, CidrBlock: sb.CidrBlock, AvailableIpCount: 251, State: "available",
		})
	}
	sort.Slice(items, func(i, j int) bool { return items[i].SubnetId < items[j].SubnetId })
	writeXML(w, http.StatusOK, rid, describeSubnetsResponse{Xmlns: ec2XMLNS, RequestId: rid, SubnetSet: subnetSetXML{Items: items}})
}

type createSecurityGroupResponse struct {
	XMLName       xml.Name  `xml:"CreateSecurityGroupResponse"`
	Xmlns         string    `xml:"xmlns,attr"`
	RequestId     string    `xml:"requestId"`
	Return        bool      `xml:"return"`
	GroupId       string    `xml:"groupId"`
	SecurityGroup sgBareXML `xml:"securityGroup"`
}

type sgBareXML struct {
	GroupId   string `xml:"groupId"`
	GroupName string `xml:"groupName"`
}

func (s *Service) createSecurityGroup(w http.ResponseWriter, q url.Values, region, rid string) {
	name := strings.TrimSpace(q.Get("GroupName"))
	desc := strings.TrimSpace(q.Get("GroupDescription"))
	vpcID := strings.TrimSpace(q.Get("VpcId"))
	if name == "" || vpcID == "" {
		writeAPIError(w, http.StatusBadRequest, "MissingParameter", "GroupName and VpcId are required", rid)
		return
	}
	st := s.store.Snapshot()
	vpc, ok := st.VPCs[vpcID]
	if !ok || vpc.Region != region {
		writeAPIError(w, http.StatusBadRequest, "InvalidVpcID.NotFound", fmt.Sprintf("The vpc ID %q does not exist", vpcID), rid)
		return
	}
	sgid := newResourceID("sg-")
	if err := s.store.Update(func(st *diskState) error {
		st.SecurityGroups[sgid] = securityGroupRec{
			ID: sgid, Region: region, VpcID: vpcID, Name: name, Description: desc, Ingress: nil,
		}
		return nil
	}); err != nil {
		writeAPIError(w, http.StatusInternalServerError, "InternalError", err.Error(), rid)
		return
	}
	writeXML(w, http.StatusOK, rid, createSecurityGroupResponse{
		Xmlns: ec2XMLNS, RequestId: rid, Return: true, GroupId: sgid,
		SecurityGroup: sgBareXML{GroupId: sgid, GroupName: name},
	})
}

type authorizeSecurityGroupIngressResponse struct {
	XMLName   xml.Name `xml:"AuthorizeSecurityGroupIngressResponse"`
	Xmlns     string   `xml:"xmlns,attr"`
	RequestId string   `xml:"requestId"`
	Return    bool     `xml:"return"`
}

func (s *Service) authorizeSecurityGroupIngress(w http.ResponseWriter, q url.Values, region, rid string) {
	sgid := strings.TrimSpace(q.Get("GroupId"))
	if sgid == "" {
		writeAPIError(w, http.StatusBadRequest, "MissingParameter", "GroupId is required", rid)
		return
	}
	rules, err := parseIpPermissions(q)
	if err != nil || len(rules) == 0 {
		writeAPIError(w, http.StatusBadRequest, "InvalidParameter", "IpPermissions required", rid)
		return
	}
	err = s.store.Update(func(st *diskState) error {
		sg, ok := st.SecurityGroups[sgid]
		if !ok || sg.Region != region {
			return errNotFoundSG
		}
		sg.Ingress = append(sg.Ingress, rules...)
		st.SecurityGroups[sgid] = sg
		return nil
	})
	if errors.Is(err, errNotFoundSG) {
		writeAPIError(w, http.StatusBadRequest, "InvalidGroup.NotFound", fmt.Sprintf("The security group %q does not exist", sgid), rid)
		return
	}
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "InternalError", err.Error(), rid)
		return
	}
	writeXML(w, http.StatusOK, rid, authorizeSecurityGroupIngressResponse{Xmlns: ec2XMLNS, RequestId: rid, Return: true})
}

type describeSecurityGroupsResponse struct {
	XMLName           xml.Name  `xml:"DescribeSecurityGroupsResponse"`
	Xmlns             string    `xml:"xmlns,attr"`
	RequestId         string    `xml:"requestId"`
	SecurityGroupInfo sgInfoSet `xml:"securityGroupInfo"`
}

type sgInfoSet struct {
	Items []sgInfoXML `xml:"item"`
}

type sgInfoXML struct {
	GroupId       string       `xml:"groupId"`
	GroupName     string       `xml:"groupName"`
	VpcId         string       `xml:"vpcId"`
	Description   string       `xml:"groupDescription"`
	IpPermissions ipPermSetXML `xml:"ipPermissions"`
}

type ipPermSetXML struct {
	Items []ipPermXML `xml:"item"`
}

type ipPermXML struct {
	IpProtocol string     `xml:"ipProtocol"`
	FromPort   int        `xml:"fromPort"`
	ToPort     int        `xml:"toPort"`
	IpRanges   ipRangeSet `xml:"ipRanges"`
}

type ipRangeSet struct {
	Items []ipRangeXML `xml:"item"`
}

type ipRangeXML struct {
	CidrIp string `xml:"cidrIp"`
}

func (s *Service) describeSecurityGroups(w http.ResponseWriter, q url.Values, region, rid string) {
	want := listIndexed(q, "GroupId")
	st := s.store.Snapshot()
	var items []sgInfoXML
	for _, sg := range st.SecurityGroups {
		if sg.Region != region {
			continue
		}
		if len(want) > 0 && !containsFold(want, sg.ID) {
			continue
		}
		var permItems []ipPermXML
		for _, r := range sg.Ingress {
			permItems = append(permItems, ipPermXML{
				IpProtocol: r.Protocol, FromPort: r.FromPort, ToPort: r.ToPort,
				IpRanges: ipRangeSet{Items: []ipRangeXML{{CidrIp: r.CidrIPv4}}},
			})
		}
		items = append(items, sgInfoXML{
			GroupId: sg.ID, GroupName: sg.Name, VpcId: sg.VpcID, Description: sg.Description,
			IpPermissions: ipPermSetXML{Items: permItems},
		})
	}
	sort.Slice(items, func(i, j int) bool { return items[i].GroupId < items[j].GroupId })
	writeXML(w, http.StatusOK, rid, describeSecurityGroupsResponse{
		Xmlns: ec2XMLNS, RequestId: rid, SecurityGroupInfo: sgInfoSet{Items: items},
	})
}

func tcpDockerPortPublish(st diskState, region string, sgIDs []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, sgid := range sgIDs {
		sg, ok := st.SecurityGroups[sgid]
		if !ok || sg.Region != region {
			continue
		}
		for _, r := range sg.Ingress {
			proto := strings.ToLower(r.Protocol)
			if proto != "tcp" || r.FromPort != r.ToPort || r.FromPort <= 0 {
				continue
			}
			p := fmt.Sprintf("%d:%d", r.FromPort, r.FromPort)
			if !seen[p] {
				seen[p] = true
				out = append(out, p)
			}
		}
	}
	sort.Strings(out)
	return out
}

func (s *Service) runInstances(w http.ResponseWriter, r *http.Request, q url.Values, region, rid string) {
	imageID := strings.TrimSpace(q.Get("ImageId"))
	subnetID := strings.TrimSpace(q.Get("SubnetId"))
	sgIDs := listIndexed(q, "SecurityGroupId")
	if len(sgIDs) == 0 {
		if g := strings.TrimSpace(q.Get("SecurityGroupId")); g != "" {
			sgIDs = []string{g}
		}
	}
	keyName := strings.TrimSpace(q.Get("KeyName"))
	if imageID == "" || subnetID == "" || len(sgIDs) == 0 {
		writeAPIError(w, http.StatusBadRequest, "MissingParameter", "ImageId, SubnetId, and SecurityGroupId are required", rid)
		return
	}
	if _, _, err := parseMinMaxCount(q); err != nil {
		writeAPIError(w, http.StatusBadRequest, "InvalidParameter", err.Error(), rid)
		return
	}

	cat, err := ReadCatalog(s.catalogPath)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "InternalError", err.Error(), rid)
		return
	}
	ami, err := cat.resolve(imageID)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "InvalidAMIID.NotFound", fmt.Sprintf("The image id %q does not exist", imageID), rid)
		return
	}

	st := s.store.Snapshot()
	sub, ok := st.Subnets[subnetID]
	if !ok || sub.Region != region {
		writeAPIError(w, http.StatusBadRequest, "InvalidSubnetID.NotFound", fmt.Sprintf("The subnet ID %q does not exist", subnetID), rid)
		return
	}
	vpcID := sub.VpcID
	for _, sgid := range sgIDs {
		sg, ok := st.SecurityGroups[sgid]
		if !ok || sg.Region != region || sg.VpcID != vpcID {
			writeAPIError(w, http.StatusBadRequest, "InvalidGroup.NotFound", fmt.Sprintf("security group %s not found or wrong vpc", sgid), rid)
			return
		}
	}
	if keyName != "" {
		if _, ok := st.KeyPairs[kpKey(region, keyName)]; !ok {
			writeAPIError(w, http.StatusBadRequest, "InvalidKeyPair.NotFound", fmt.Sprintf("The key pair %q does not exist", keyName), rid)
			return
		}
	}

	tags := parseTagSpecifications(q)
	iid := newResourceID("i-")
	cname := "twister-ec2-" + strings.ReplaceAll(iid, ":", "-")
	ports := tcpDockerPortPublish(st, region, sgIDs)
	privateIP := "10.0.1.10"
	publicIP := s.publicHost

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Minute)
	defer cancel()
	if err := dockerPull(ctx, ami.DockerImage); err != nil {
		writeAPIError(w, http.StatusInternalServerError, "InternalError", err.Error(), rid)
		return
	}
	if err := dockerRunDetached(ctx, cname, ami.DockerImage, ami.Command, ports); err != nil {
		writeAPIError(w, http.StatusInternalServerError, "InternalError", err.Error(), rid)
		return
	}

	inst := instanceRec{
		ID:               iid,
		Region:           region,
		AMI:              imageID,
		SubnetID:         subnetID,
		SecurityGroupIDs: append([]string(nil), sgIDs...),
		State:            "running",
		PrivateIPAddress: privateIP,
		PublicIPAddress:  publicIP,
		DockerImage:      ami.DockerImage,
		DockerCommand:    slices.Clone(ami.Command),
		ContainerName:    cname,
		User:             ami.DefaultUser,
		Tags:             mapsClone(tags),
	}
	if err := s.store.Update(func(st *diskState) error {
		st.Instances[iid] = inst
		return nil
	}); err != nil {
		_ = dockerStopRemove(context.Background(), cname)
		writeAPIError(w, http.StatusInternalServerError, "InternalError", err.Error(), rid)
		return
	}

	writeXML(w, http.StatusOK, rid, runInstancesResponseXML(rid, newResourceID("r-"), []instanceRec{inst}, st))
}

func mapsClone(m map[string]string) map[string]string {
	if m == nil {
		return map[string]string{}
	}
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

type runInstancesResponse struct {
	XMLName       xml.Name        `xml:"RunInstancesResponse"`
	Xmlns         string          `xml:"xmlns,attr"`
	RequestId     string          `xml:"requestId"`
	ReservationId string          `xml:"reservationId"`
	OwnerId       string          `xml:"ownerId"`
	Instances     instancesSetXML `xml:"instancesSet"`
}

type instancesSetXML struct {
	Items []instanceItemXML `xml:"item"`
}

type instanceItemXML struct {
	InstanceId       string           `xml:"instanceId"`
	ImageId          string           `xml:"imageId"`
	SubnetId         string           `xml:"subnetId"`
	InstanceState    instanceStateXML `xml:"instanceState"`
	PrivateIpAddress string           `xml:"privateIpAddress"`
	PublicIpAddress  string           `xml:"publicIpAddress,omitempty"`
	SecurityGroups   instSGSetXML     `xml:"groupSet"`
	TagSet           tagSetXML        `xml:"tagSet,omitempty"`
}

type instSGSetXML struct {
	Items []instSGItemXML `xml:"item"`
}

type instSGItemXML struct {
	GroupId   string `xml:"groupId"`
	GroupName string `xml:"groupName"`
}

type instanceStateXML struct {
	Code int    `xml:"code"`
	Name string `xml:"name"`
}

type tagSetXML struct {
	Items []tagItemXML `xml:"item"`
}

type tagItemXML struct {
	Key   string `xml:"key"`
	Value string `xml:"value"`
}

func buildInstanceItemList(insts []instanceRec, st diskState) []instanceItemXML {
	var items []instanceItemXML
	for _, in := range insts {
		var sgs []instSGItemXML
		for _, gid := range in.SecurityGroupIDs {
			gname := "default"
			if sg, ok := st.SecurityGroups[gid]; ok {
				gname = sg.Name
			}
			sgs = append(sgs, instSGItemXML{GroupId: gid, GroupName: gname})
		}
		var tagItems []tagItemXML
		for k, v := range in.Tags {
			tagItems = append(tagItems, tagItemXML{Key: k, Value: v})
		}
		sort.Slice(tagItems, func(i, j int) bool { return tagItems[i].Key < tagItems[j].Key })
		items = append(items, instanceItemXML{
			InstanceId:       in.ID,
			ImageId:          in.AMI,
			SubnetId:         in.SubnetID,
			InstanceState:    instanceStateXML{Code: stateCode(in.State), Name: in.State},
			PrivateIpAddress: in.PrivateIPAddress,
			PublicIpAddress:  in.PublicIPAddress,
			SecurityGroups:   instSGSetXML{Items: sgs},
			TagSet:           tagSetXML{Items: tagItems},
		})
	}
	return items
}

func runInstancesResponseXML(rid, resvID string, insts []instanceRec, st diskState) runInstancesResponse {
	return runInstancesResponse{
		Xmlns:         ec2XMLNS,
		RequestId:     rid,
		ReservationId: resvID,
		OwnerId:       "000000000000",
		Instances:     instancesSetXML{Items: buildInstanceItemList(insts, st)},
	}
}

func stateCode(name string) int {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "pending":
		return 0
	case "running":
		return 16
	case "shutting-down":
		return 32
	case "terminated":
		return 48
	case "stopping":
		return 64
	case "stopped":
		return 80
	default:
		return 16
	}
}

func instanceMatchesFilter(inst instanceRec, f nameValuesFilter) bool {
	if len(f.Values) == 0 {
		return true
	}
	switch f.Name {
	case "instance-state-name":
		for _, v := range f.Values {
			if strings.EqualFold(v, inst.State) {
				return true
			}
		}
		return false
	case "tag:Name":
		for _, v := range f.Values {
			if inst.Tags["Name"] == v {
				return true
			}
		}
		return false
	case "resource-id":
		for _, v := range f.Values {
			if strings.EqualFold(v, inst.ID) {
				return true
			}
		}
		return false
	default:
		return true
	}
}

func filterByFilters(insts []instanceRec, filters []nameValuesFilter) []instanceRec {
	out := insts
	for _, f := range filters {
		var next []instanceRec
		for _, inst := range out {
			if instanceMatchesFilter(inst, f) {
				next = append(next, inst)
			}
		}
		out = next
	}
	return out
}

type describeInstancesResponse struct {
	XMLName        xml.Name          `xml:"DescribeInstancesResponse"`
	Xmlns          string            `xml:"xmlns,attr"`
	RequestId      string            `xml:"requestId"`
	ReservationSet reservationSetXML `xml:"reservationSet"`
}

type reservationSetXML struct {
	Items []reservationItemXML `xml:"item"`
}

type reservationItemXML struct {
	ReservationId string          `xml:"reservationId"`
	OwnerId       string          `xml:"ownerId"`
	Instances     instancesSetXML `xml:"instancesSet"`
}

func (s *Service) describeInstances(w http.ResponseWriter, q url.Values, region, rid string) {
	st := s.store.Snapshot()
	var candidates []instanceRec
	for _, in := range st.Instances {
		if in.Region != region {
			continue
		}
		candidates = append(candidates, in)
	}

	ids := listIndexed(q, "InstanceId")
	if len(ids) > 0 {
		want := map[string]struct{}{}
		for _, id := range ids {
			want[strings.ToLower(strings.TrimSpace(id))] = struct{}{}
		}
		var narrowed []instanceRec
		for _, in := range candidates {
			if _, ok := want[strings.ToLower(in.ID)]; ok {
				narrowed = append(narrowed, in)
			}
		}
		candidates = narrowed
	}

	candidates = filterByFilters(candidates, parseFilters(q))

	sort.Slice(candidates, func(i, j int) bool { return candidates[i].ID < candidates[j].ID })

	resID := newResourceID("r-")
	item := reservationItemXML{
		ReservationId: resID,
		OwnerId:       "000000000000",
		Instances:     instancesSetXML{Items: buildInstanceItemList(candidates, st)},
	}
	writeXML(w, http.StatusOK, rid, describeInstancesResponse{
		Xmlns: ec2XMLNS, RequestId: rid, ReservationSet: reservationSetXML{Items: []reservationItemXML{item}},
	})
}

var errInstNotFound = errors.New("ec2: instance not found")

type terminateInstancesResponse struct {
	XMLName   xml.Name         `xml:"TerminateInstancesResponse"`
	Xmlns     string           `xml:"xmlns,attr"`
	RequestId string           `xml:"requestId"`
	Instances termInstancesXML `xml:"instancesSet"`
}

type termInstancesXML struct {
	Items []termInstanceItemXML `xml:"item"`
}

type termInstanceItemXML struct {
	InstanceId    string           `xml:"instanceId"`
	CurrentState  instanceStateXML `xml:"currentState"`
	PreviousState instanceStateXML `xml:"previousState"`
}

func (s *Service) terminateInstances(w http.ResponseWriter, r *http.Request, q url.Values, region, rid string) {
	ids := listIndexed(q, "InstanceId")
	if len(ids) == 0 {
		writeAPIError(w, http.StatusBadRequest, "MissingParameter", "InstanceId is required", rid)
		return
	}

	snap := s.store.Snapshot()
	for _, id := range ids {
		inst, ok := snap.Instances[id]
		if !ok || inst.Region != region {
			writeAPIError(w, http.StatusBadRequest, "InvalidInstanceID.NotFound", fmt.Sprintf("The instance ID %q does not exist", id), rid)
			return
		}
	}

	var out []termInstanceItemXML
	for _, id := range ids {
		var prevName, cname string
		var prevCode int
		err := s.store.Update(func(st *diskState) error {
			inst, ok := st.Instances[id]
			if !ok || inst.Region != region {
				return errInstNotFound
			}
			prevName = inst.State
			prevCode = stateCode(inst.State)
			cname = inst.ContainerName
			if inst.State != "terminated" {
				inst.State = "terminated"
				st.Instances[id] = inst
			}
			return nil
		})
		if err != nil {
			if errors.Is(err, errInstNotFound) {
				writeAPIError(w, http.StatusBadRequest, "InvalidInstanceID.NotFound", fmt.Sprintf("The instance ID %q does not exist", id), rid)
				return
			}
			writeAPIError(w, http.StatusInternalServerError, "InternalError", err.Error(), rid)
			return
		}
		if cname != "" && prevName != "terminated" {
			_ = dockerStopRemove(r.Context(), cname)
		}
		out = append(out, termInstanceItemXML{
			InstanceId:    id,
			PreviousState: instanceStateXML{Code: prevCode, Name: prevName},
			CurrentState:  instanceStateXML{Code: 48, Name: "terminated"},
		})
	}

	writeXML(w, http.StatusOK, rid, terminateInstancesResponse{
		Xmlns: ec2XMLNS, RequestId: rid, Instances: termInstancesXML{Items: out},
	})
}

type stopInstancesResponse struct {
	XMLName   xml.Name         `xml:"StopInstancesResponse"`
	Xmlns     string           `xml:"xmlns,attr"`
	RequestId string           `xml:"requestId"`
	Instances termInstancesXML `xml:"instancesSet"`
}

func (s *Service) stopInstances(w http.ResponseWriter, r *http.Request, q url.Values, region, rid string) {
	ids := listIndexed(q, "InstanceId")
	if len(ids) == 0 {
		writeAPIError(w, http.StatusBadRequest, "MissingParameter", "InstanceId is required", rid)
		return
	}
	snap := s.store.Snapshot()
	for _, id := range ids {
		inst, ok := snap.Instances[id]
		if !ok || inst.Region != region {
			writeAPIError(w, http.StatusBadRequest, "InvalidInstanceID.NotFound", fmt.Sprintf("The instance ID %q does not exist", id), rid)
			return
		}
		if inst.State == "terminated" {
			writeAPIError(w, http.StatusBadRequest, "IncorrectInstanceState", fmt.Sprintf("The instance %q is terminated and cannot be stopped", id), rid)
			return
		}
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Minute)
	defer cancel()

	var out []termInstanceItemXML
	for _, id := range ids {
		inst := snap.Instances[id]
		prevName := inst.State
		prevCode := stateCode(prevName)

		if prevName == "stopped" {
			out = append(out, termInstanceItemXML{
				InstanceId:    id,
				PreviousState: instanceStateXML{Code: 80, Name: "stopped"},
				CurrentState:  instanceStateXML{Code: 80, Name: "stopped"},
			})
			continue
		}

		if err := dockerStopOnly(ctx, inst.ContainerName); err != nil {
			writeAPIError(w, http.StatusInternalServerError, "InternalError", err.Error(), rid)
			return
		}
		err := s.store.Update(func(st *diskState) error {
			in, ok := st.Instances[id]
			if !ok || in.Region != region {
				return errInstNotFound
			}
			if in.State != "stopped" && in.State != "terminated" {
				in.State = "stopped"
				st.Instances[id] = in
			}
			return nil
		})
		if err != nil {
			if errors.Is(err, errInstNotFound) {
				writeAPIError(w, http.StatusBadRequest, "InvalidInstanceID.NotFound", fmt.Sprintf("The instance ID %q does not exist", id), rid)
				return
			}
			writeAPIError(w, http.StatusInternalServerError, "InternalError", err.Error(), rid)
			return
		}
		out = append(out, termInstanceItemXML{
			InstanceId:    id,
			PreviousState: instanceStateXML{Code: prevCode, Name: prevName},
			CurrentState:  instanceStateXML{Code: 80, Name: "stopped"},
		})
	}

	writeXML(w, http.StatusOK, rid, stopInstancesResponse{
		Xmlns: ec2XMLNS, RequestId: rid, Instances: termInstancesXML{Items: out},
	})
}

type startInstancesResponse struct {
	XMLName   xml.Name         `xml:"StartInstancesResponse"`
	Xmlns     string           `xml:"xmlns,attr"`
	RequestId string           `xml:"requestId"`
	Instances termInstancesXML `xml:"instancesSet"`
}

func (s *Service) resolveInstanceDockerCommand(inst *instanceRec) ([]string, error) {
	if len(inst.DockerCommand) > 0 {
		return slices.Clone(inst.DockerCommand), nil
	}
	cat, err := ReadCatalog(s.catalogPath)
	if err != nil {
		return nil, err
	}
	ent, err := cat.resolve(inst.AMI)
	if err != nil {
		return []string{"/bin/sleep", "infinity"}, nil
	}
	return slices.Clone(ent.Command), nil
}

func isDockerNoSuchContainerErr(err error) bool {
	if err == nil {
		return false
	}
	m := strings.ToLower(err.Error())
	return strings.Contains(m, "no such container")
}

func (s *Service) ensureInstanceContainerRunning(ctx context.Context, inst *instanceRec, st diskState) error {
	if err := dockerStart(ctx, inst.ContainerName); err != nil {
		if !isDockerNoSuchContainerErr(err) {
			return err
		}
		cmd, err := s.resolveInstanceDockerCommand(inst)
		if err != nil {
			return err
		}
		if err := dockerPull(ctx, inst.DockerImage); err != nil {
			return err
		}
		ports := tcpDockerPortPublish(st, inst.Region, inst.SecurityGroupIDs)
		return dockerRunDetached(ctx, inst.ContainerName, inst.DockerImage, cmd, ports)
	}
	return nil
}

func (s *Service) startInstances(w http.ResponseWriter, r *http.Request, q url.Values, region, rid string) {
	ids := listIndexed(q, "InstanceId")
	if len(ids) == 0 {
		writeAPIError(w, http.StatusBadRequest, "MissingParameter", "InstanceId is required", rid)
		return
	}
	snap := s.store.Snapshot()
	for _, id := range ids {
		inst, ok := snap.Instances[id]
		if !ok || inst.Region != region {
			writeAPIError(w, http.StatusBadRequest, "InvalidInstanceID.NotFound", fmt.Sprintf("The instance ID %q does not exist", id), rid)
			return
		}
		switch inst.State {
		case "terminated":
			writeAPIError(w, http.StatusBadRequest, "IncorrectInstanceState", fmt.Sprintf("The instance %q is terminated", id), rid)
			return
		case "running", "pending":
			writeAPIError(w, http.StatusBadRequest, "IncorrectInstanceState", fmt.Sprintf("The instance %q is already running", id), rid)
			return
		case "stopped":
		default:
			writeAPIError(w, http.StatusBadRequest, "IncorrectInstanceState", fmt.Sprintf("The instance %q is not in a state from which it can be started", id), rid)
			return
		}
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Minute)
	defer cancel()

	var out []termInstanceItemXML
	for _, id := range ids {
		inst := snap.Instances[id]
		st := s.store.Snapshot()
		if err := s.ensureInstanceContainerRunning(ctx, &inst, st); err != nil {
			writeAPIError(w, http.StatusInternalServerError, "InternalError", err.Error(), rid)
			return
		}
		err := s.store.Update(func(d *diskState) error {
			in, ok := d.Instances[id]
			if !ok || in.Region != region {
				return errInstNotFound
			}
			if in.State == "stopped" {
				in.State = "running"
				d.Instances[id] = in
			}
			return nil
		})
		if err != nil {
			if errors.Is(err, errInstNotFound) {
				writeAPIError(w, http.StatusBadRequest, "InvalidInstanceID.NotFound", fmt.Sprintf("The instance ID %q does not exist", id), rid)
				return
			}
			writeAPIError(w, http.StatusInternalServerError, "InternalError", err.Error(), rid)
			return
		}
		out = append(out, termInstanceItemXML{
			InstanceId:    id,
			PreviousState: instanceStateXML{Code: 80, Name: "stopped"},
			CurrentState:  instanceStateXML{Code: 16, Name: "running"},
		})
	}

	writeXML(w, http.StatusOK, rid, startInstancesResponse{
		Xmlns: ec2XMLNS, RequestId: rid, Instances: termInstancesXML{Items: out},
	})
}
