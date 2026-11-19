package auth

import "testing"

func TestHashAndVerifyPassword(t *testing.T) {
	hash, err := HashPassword("correct horse battery staple")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	if !ValidHash(hash) {
		t.Fatalf("ValidHash rejected generated hash: %s", hash)
	}
	if !VerifyPassword(hash, "correct horse battery staple") {
		t.Fatal("VerifyPassword rejected the correct password")
	}
	if VerifyPassword(hash, "wrong password") {
		t.Fatal("VerifyPassword accepted a wrong password")
	}
}

func TestHashPasswordRejectsWeakPassword(t *testing.T) {
	if _, err := HashPassword("short"); err == nil {
		t.Fatal("expected a weak password error")
	}
}

func TestValidHashRejectsMalformedInput(t *testing.T) {
	cases := []string{
		"",
		"plain",
		"pbkdf2-sha256:1:00:00",
		"bcrypt:600000:0011223344556677:00112233445566778899aabbccddeeff",
		"pbkdf2-sha256:600000:zz:00112233445566778899aabbccddeeff",
	}
	for _, tc := range cases {
		if ValidHash(tc) {
			t.Fatalf("ValidHash accepted malformed hash %q", tc)
		}
	}
}
