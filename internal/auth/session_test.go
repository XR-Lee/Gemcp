package auth

import (
	"testing"
	"time"

	"github.com/XR-Lee/Gemcp/ent"
	"github.com/XR-Lee/Gemcp/internal/secrets"
)

func TestValidateCSRF(t *testing.T) {
	key, _ := secrets.GenerateMasterKey()
	box, _ := secrets.New(key)
	service := NewService(nil, box, time.Hour)
	record := &ent.Session{CsrfHash: box.Digest("csrf", "expected")}
	if err := service.ValidateCSRF(record, "expected"); err != nil {
		t.Fatalf("ValidateCSRF() error = %v", err)
	}
	if err := service.ValidateCSRF(record, "wrong"); err == nil {
		t.Fatal("ValidateCSRF() accepted wrong token")
	}
}
