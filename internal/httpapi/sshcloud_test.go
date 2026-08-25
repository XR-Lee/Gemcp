package httpapi

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"entgo.io/ent/dialect"
	"github.com/XR-Lee/Gemcp/ent/enttest"
	"github.com/XR-Lee/Gemcp/internal/auth"
	"github.com/XR-Lee/Gemcp/internal/secrets"
	"github.com/XR-Lee/Gemcp/internal/sshcloud"
	"github.com/gin-gonic/gin"
	_ "github.com/mattn/go-sqlite3"
)

func TestSSHCloudHTTPOmitsCredentialMaterial(t *testing.T) {
	gin.SetMode(gin.TestMode)
	client := enttest.Open(t, dialect.SQLite, "file:ssh-cloud-http?mode=memory&cache=shared&_fk=1")
	defer client.Close()
	ctx := context.Background()
	tenant, _ := client.Tenant.Create().SetName("Test").Save(ctx)
	key, _ := secrets.GenerateMasterKey()
	box, _ := secrets.New(key)
	config := sshcloud.DefaultConfig()
	config.Enabled = true
	config.InstanceID = "http-test"
	service, err := sshcloud.NewService(client, box, config)
	if err != nil {
		t.Fatal(err)
	}
	service.WithDial(func(context.Context, sshcloud.Target, sshcloud.Credential, string) (sshcloud.Conn, string, error) {
		return &scriptedSSHConn{script: map[string]string{
			"uname":         "Linux x86_64",
			"ServerVersion": "27.0.3",
			"Runtimes":      "runc nvidia",
			"nvidia-smi":    "NVIDIA GeForce RTX 4090, GPU-12345678-1234-1234-1234-123456789abc, 24576",
		}}, "SHA256:http-fingerprint", nil
	})
	handlers := NewSSHCloudHandlers(service)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(principalContextKey, authenticatedContext{Principal: auth.Principal{
			TenantID: tenant.ID, TenantPublicID: tenant.PublicID.String(), UserPublicID: "owner", Role: "owner",
		}})
		c.Next()
	})
	router.GET("/ssh-cloud-nodes", handlers.List)
	router.POST("/ssh-cloud-nodes", handlers.Create)

	create := jsonRequest(t, http.MethodPost, "/ssh-cloud-nodes", sshcloud.CreateInput{
		Label: "lab-cloud", Host: "203.0.113.20", User: "ubuntu", AuthMethod: "password", Password: "do-not-echo-this-password",
	})
	createResponse := httptest.NewRecorder()
	router.ServeHTTP(createResponse, create)
	if createResponse.Code != http.StatusCreated {
		t.Fatalf("create status=%d body=%s", createResponse.Code, createResponse.Body.String())
	}
	body := createResponse.Body.String()
	for _, forbidden := range []string{"do-not-echo-this-password", "BEGIN", "ciphertext", "private_key"} {
		if strings.Contains(strings.ToLower(body), strings.ToLower(forbidden)) {
			t.Fatalf("create response leaked %q: %s", forbidden, body)
		}
	}
	var created struct {
		Data sshcloud.NodeView `json:"data"`
	}
	if err := json.Unmarshal(createResponse.Body.Bytes(), &created); err != nil || created.Data.ID == "" || created.Data.Warning == "" {
		t.Fatalf("create payload=%s err=%v", createResponse.Body.String(), err)
	}

	list := httptest.NewRecorder()
	router.ServeHTTP(list, httptest.NewRequest(http.MethodGet, "/ssh-cloud-nodes", nil))
	if list.Code != http.StatusOK || strings.Contains(list.Body.String(), "do-not-echo-this-password") || !strings.Contains(list.Body.String(), "experimental") {
		t.Fatalf("list status=%d body=%s", list.Code, list.Body.String())
	}
}

type scriptedSSHConn struct {
	script map[string]string
}

func (c *scriptedSSHConn) Run(_ context.Context, command string, _ int) (string, error) {
	for prefix, output := range c.script {
		if strings.Contains(command, prefix) {
			return output, nil
		}
	}
	return "", nil
}

func (c *scriptedSSHConn) Upload(context.Context, string, io.Reader) error { return nil }
func (c *scriptedSSHConn) Close() error                                    { return nil }
