package ec2

import (
	"context"
	"encoding/xml"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func stubDockerAll(t *testing.T) func() {
	t.Helper()
	orig := dockerExec
	dockerExec = func(ctx context.Context, args ...string) ([]byte, error) {
		return []byte("stubbed\n"), nil
	}
	return func() { dockerExec = orig }
}

func newTestCatalog(t *testing.T, root string) string {
	t.Helper()
	p := filepath.Join(root, "ec2-ami-catalog.json")
	body := `{"amis":{"ami-test":{"dockerImage":"busybox:latest","command":["sleep","3600"],"defaultUser":"root"}}}`
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func post(svc *Service, region, form string) *httptest.ResponseRecorder {
	rr := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/", strings.NewReader(form))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded; charset=utf-8")
	svc.Handle(rr, r, region, []byte(form), "test-req")
	return rr
}

func TestCreateKeyPair(t *testing.T) {
	dir := t.TempDir()
	svc, err := NewService(dir, newTestCatalog(t, dir), "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	rr := post(svc, "eu-west-1", "Action=CreateKeyPair&Version=2016-11-15&KeyName=mykey")
	if rr.Code != 200 {
		t.Fatalf("code %d %s", rr.Code, rr.Body.String())
	}
	var parsed struct {
		KeyName        string `xml:"keyName"`
		KeyFingerprint string `xml:"keyFingerprint"`
		KeyMaterial    string `xml:"keyMaterial"`
	}
	if err := xml.Unmarshal(rr.Body.Bytes(), &parsed); err != nil {
		t.Fatal(err)
	}
	if parsed.KeyName != "mykey" || !strings.Contains(parsed.KeyMaterial, "BEGIN RSA PRIVATE KEY") {
		t.Fatalf("response: %+v", parsed)
	}

	rr2 := post(svc, "eu-west-1", "Action=CreateKeyPair&Version=2016-11-15&KeyName=mykey")
	if rr2.Code != 400 {
		t.Fatalf("duplicate expected 400, got %d %s", rr2.Code, rr2.Body.String())
	}
}

func TestSecurityGroupIngressAndRunInstances_stubDocker(t *testing.T) {
	defer stubDockerAll(t)()
	dir := t.TempDir()
	svc, err := NewService(dir, newTestCatalog(t, dir), "10.0.0.5")
	if err != nil {
		t.Fatal(err)
	}
	region := "us-east-1"

	rr := post(svc, region, "Action=CreateVpc&Version=2016-11-15")
	if rr.Code != 200 {
		t.Fatal(rr.Body.String())
	}
	var cv struct {
		VPC struct {
			VpcID string `xml:"vpcId"`
		} `xml:"vpc"`
	}
	if err := xml.Unmarshal(rr.Body.Bytes(), &cv); err != nil {
		t.Fatal(err)
	}
	vpcID := cv.VPC.VpcID

	rr = post(svc, region, "Action=CreateSubnet&Version=2016-11-15&VpcId="+vpcID+"&CidrBlock=10.0.1.0/24")
	if rr.Code != 200 {
		t.Fatal(rr.Body.String())
	}
	var cs struct {
		Subnet struct {
			SubnetID string `xml:"subnetId"`
		} `xml:"subnet"`
	}
	if err := xml.Unmarshal(rr.Body.Bytes(), &cs); err != nil {
		t.Fatal(err)
	}
	subID := cs.Subnet.SubnetID

	rr = post(svc, region, "Action=CreateSecurityGroup&Version=2016-11-15&GroupName=g&GroupDescription=d&VpcId="+vpcID)
	if rr.Code != 200 {
		t.Fatal(rr.Body.String())
	}
	var csg struct {
		GroupID string `xml:"groupId"`
	}
	if err := xml.Unmarshal(rr.Body.Bytes(), &csg); err != nil {
		t.Fatal(err)
	}
	sgID := csg.GroupID

	rr = post(svc, region, "Action=AuthorizeSecurityGroupIngress&Version=2016-11-15&GroupId="+sgID+
		"&IpPermissions.1.IpProtocol=tcp&IpPermissions.1.FromPort=8080&IpPermissions.1.ToPort=8080&IpPermissions.1.IpRanges.1.CidrIp=0.0.0.0/0")
	if rr.Code != 200 {
		t.Fatal(rr.Body.String())
	}

	post(svc, region, "Action=CreateKeyPair&Version=2016-11-15&KeyName=ssh")

	runForm := "Action=RunInstances&Version=2016-11-15&ImageId=ami-test&MinCount=1&MaxCount=1&SubnetId=" + subID +
		"&SecurityGroupId.1=" + sgID + "&KeyName=ssh"
	rr = post(svc, region, runForm)
	if rr.Code != 200 {
		t.Fatalf("run: %d %s", rr.Code, rr.Body.String())
	}
	var runParsed struct {
		Instances struct {
			Items []struct {
				ID        string `xml:"instanceId"`
				PublicIP  string `xml:"publicIpAddress"`
				PrivateIP string `xml:"privateIpAddress"`
			} `xml:"item"`
		} `xml:"instancesSet"`
	}
	if err := xml.Unmarshal(rr.Body.Bytes(), &runParsed); err != nil {
		t.Fatal(err)
	}
	if len(runParsed.Instances.Items) != 1 {
		t.Fatalf("instances: %+v", runParsed)
	}
	instID := runParsed.Instances.Items[0].ID
	if runParsed.Instances.Items[0].PublicIP != "10.0.0.5" {
		t.Fatalf("public ip: %q", runParsed.Instances.Items[0].PublicIP)
	}

	rr = post(svc, region, "Action=DescribeInstances&Version=2016-11-15&InstanceId.1="+instID+
		"&Filters.1.Name=instance-state-name&Filters.1.Values.1=running")
	if rr.Code != 200 {
		t.Fatal(rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), instID) {
		t.Fatal("describe missing instance")
	}

	rr = post(svc, region, "Action=TerminateInstances&Version=2016-11-15&InstanceId.1="+instID)
	if rr.Code != 200 {
		t.Fatal(rr.Body.String())
	}
}

func TestStopStartInstances_stubDocker(t *testing.T) {
	defer stubDockerAll(t)()
	dir := t.TempDir()
	svc, err := NewService(dir, newTestCatalog(t, dir), "10.0.0.5")
	if err != nil {
		t.Fatal(err)
	}
	region := "us-east-1"

	rr := post(svc, region, "Action=CreateVpc&Version=2016-11-15")
	if rr.Code != 200 {
		t.Fatal(rr.Body.String())
	}
	var cv struct {
		VPC struct {
			VpcID string `xml:"vpcId"`
		} `xml:"vpc"`
	}
	if err := xml.Unmarshal(rr.Body.Bytes(), &cv); err != nil {
		t.Fatal(err)
	}
	rr = post(svc, region, "Action=CreateSubnet&Version=2016-11-15&VpcId="+cv.VPC.VpcID+"&CidrBlock=10.0.1.0/24")
	if rr.Code != 200 {
		t.Fatal(rr.Body.String())
	}
	var cs struct {
		Subnet struct {
			SubnetID string `xml:"subnetId"`
		} `xml:"subnet"`
	}
	if err := xml.Unmarshal(rr.Body.Bytes(), &cs); err != nil {
		t.Fatal(err)
	}
	rr = post(svc, region, "Action=CreateSecurityGroup&Version=2016-11-15&GroupName=g2&GroupDescription=d&VpcId="+cv.VPC.VpcID)
	if rr.Code != 200 {
		t.Fatal(rr.Body.String())
	}
	var csg struct {
		GroupID string `xml:"groupId"`
	}
	if err := xml.Unmarshal(rr.Body.Bytes(), &csg); err != nil {
		t.Fatal(err)
	}
	post(svc, region, "Action=CreateKeyPair&Version=2016-11-15&KeyName=k2")

	runForm := "Action=RunInstances&Version=2016-11-15&ImageId=ami-test&MinCount=1&MaxCount=1&SubnetId=" + cs.Subnet.SubnetID +
		"&SecurityGroupId.1=" + csg.GroupID + "&KeyName=k2"
	rr = post(svc, region, runForm)
	if rr.Code != 200 {
		t.Fatalf("run: %d %s", rr.Code, rr.Body.String())
	}
	var runParsed struct {
		Instances struct {
			Items []struct {
				ID string `xml:"instanceId"`
			} `xml:"item"`
		} `xml:"instancesSet"`
	}
	if err := xml.Unmarshal(rr.Body.Bytes(), &runParsed); err != nil {
		t.Fatal(err)
	}
	instID := runParsed.Instances.Items[0].ID

	rr = post(svc, region, "Action=StopInstances&Version=2016-11-15&InstanceId.1="+instID)
	if rr.Code != 200 || !strings.Contains(rr.Body.String(), "stopped") {
		t.Fatalf("stop: %d %s", rr.Code, rr.Body.String())
	}

	rr = post(svc, region, "Action=StartInstances&Version=2016-11-15&InstanceId.1="+instID)
	if rr.Code != 200 || !strings.Contains(rr.Body.String(), "running") {
		t.Fatalf("start: %d %s", rr.Code, rr.Body.String())
	}

	rr = post(svc, region, "Action=StartInstances&Version=2016-11-15&InstanceId.1="+instID)
	if rr.Code != 400 {
		t.Fatalf("start when running want 400, got %d %s", rr.Code, rr.Body.String())
	}
}
