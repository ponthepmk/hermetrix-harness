package main

import "testing"

func TestNonLoopbackListenerRequiresAuthenticationAndTLS(t *testing.T) {
	for _, test := range []struct {
		auth, tls bool
		wantErr   bool
	}{
		{false, false, true}, {true, false, true}, {false, true, true}, {true, true, false},
	} {
		err := requireSecureListener("0.0.0.0:7331", test.auth, test.tls)
		if (err != nil) != test.wantErr {
			t.Errorf("auth=%v tls=%v err=%v wantErr=%v", test.auth, test.tls, err, test.wantErr)
		}
	}
	if err := requireSecureListener("127.0.0.1:7331", false, false); err != nil {
		t.Fatalf("loopback listener was rejected: %v", err)
	}
}
