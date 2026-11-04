package oss

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	alioss "github.com/aliyun/alibabacloud-oss-go-sdk-v2/oss"
	"github.com/aliyun/alibabacloud-oss-go-sdk-v2/oss/credentials"
	"github.com/solarhell/certship/pkg/ent"
	"go.uber.org/zap"
)

func scannerAt(endpoint string) *Scanner {
	s := NewScanner(zap.NewNop())
	s.client = func(_, _, region string) *alioss.Client {
		return alioss.NewClient(alioss.LoadDefaultConfig().WithRegion(region).WithEndpoint(endpoint).
			WithUsePathStyle(true).WithCredentialsProvider(credentials.NewStaticCredentialsProvider("test", "test")))
	}
	return s
}

func TestScanAccountCompleteness(t *testing.T) {
	for _, code := range []string{"NoSuchCname", "AccessDenied", "NoSuchBucket"} {
		t.Run(code, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/xml")
				if strings.Trim(r.URL.Path, "/") == "" {
					fmt.Fprint(w, `<ListAllMyBucketsResult><Buckets><Bucket><Name>bucket</Name><Location>oss-cn-hangzhou</Location></Bucket></Buckets><IsTruncated>false</IsTruncated></ListAllMyBucketsResult>`)
					return
				}
				status := http.StatusForbidden
				if code != "AccessDenied" {
					status = http.StatusNotFound
				}
				w.WriteHeader(status)
				fmt.Fprintf(w, `<Error><Code>%s</Code><Message>fixture</Message><RequestId>test</RequestId></Error>`, code)
			}))
			defer server.Close()
			result, err := scannerAt(server.URL).ScanAccount(context.Background(), &ent.CloudAccount{})
			if err != nil {
				t.Fatal(err)
			}
			if len(result.Buckets) != 1 || len(result.Domains) != 0 {
				t.Fatalf("unexpected result: %+v", result)
			}
			if got := len(result.CoveredBuckets) == 1; got != (code == "NoSuchCname") {
				t.Fatalf("%s completeness=%v", code, got)
			}
		})
	}
}

func TestLastBucketPageFailureDiscardsPartialInventory(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		if strings.Trim(r.URL.Path, "/") != "" {
			fmt.Fprint(w, `<ListCnameResult><Cname><Domain>live.example.com</Domain><Status>Enabled</Status></Cname></ListCnameResult>`)
			return
		}
		if r.URL.Query().Get("marker") == "next" {
			w.WriteHeader(http.StatusForbidden)
			fmt.Fprint(w, `<Error><Code>AccessDenied</Code><Message>fixture</Message><RequestId>test</RequestId></Error>`)
			return
		}
		fmt.Fprint(w, `<ListAllMyBucketsResult><Buckets><Bucket><Name>first</Name><Location>oss-cn-hangzhou</Location></Bucket></Buckets><IsTruncated>true</IsTruncated><NextMarker>next</NextMarker></ListAllMyBucketsResult>`)
	}))
	defer server.Close()
	result, err := scannerAt(server.URL).ScanAccount(context.Background(), &ent.CloudAccount{})
	if err == nil || len(result.Buckets) != 0 || len(result.Domains) != 0 {
		t.Fatalf("partial page accepted: %+v %v", result, err)
	}
}

func TestCertificateObservationDistinguishesUnknownAndDisabled(t *testing.T) {
	for _, status := range []string{"Disabled", "Enabled"} {
		t.Run(status, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/xml")
				fmt.Fprintf(w, `<ListCnameResult><Cname><Domain>test.example.com</Domain><Status>Enabled</Status><Certificate><Status>%s</Status><ValidEndDate>bad-date</ValidEndDate></Certificate></Cname></ListCnameResult>`, status)
			}))
			defer server.Close()
			result, err := scannerAt(server.URL).listBucketCnames(context.Background(), &ent.CloudAccount{}, "bucket", "cn-hangzhou")
			if err != nil || len(result) != 1 {
				t.Fatalf("read cname: %v %v", result, err)
			}
			if result[0].CertKnown != (status == "Disabled") || result[0].OSSCert != nil {
				t.Fatalf("misclassified certificate: %+v", result[0])
			}
		})
	}
}
