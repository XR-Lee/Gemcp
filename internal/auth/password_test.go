package auth

import "testing"

func TestPasswordRoundTrip(t *testing.T) {
	hash, err := HashPassword("correct horse battery staple")
	if err != nil {
		t.Fatalf("HashPassword() error = %v", err)
	}
	if !VerifyPassword(hash, "correct horse battery staple") {
		t.Fatal("VerifyPassword() rejected valid password")
	}
	if VerifyPassword(hash, "incorrect password") {
		t.Fatal("VerifyPassword() accepted invalid password")
	}
}

func TestPasswordMinimum(t *testing.T) {
	if _, err := HashPassword("short"); err == nil {
		t.Fatal("HashPassword() accepted short password")
	}
}
