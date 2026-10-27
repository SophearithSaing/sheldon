package app

import (
	"testing"

	"golang.org/x/sys/windows"
)

func TestPrivateACLRejectsOtherUsers(t *testing.T) {
	sa, err := privateSecurity()
	if err != nil {
		t.Fatal(err)
	}
	if err := checkPrivateSecurity(sa.SecurityDescriptor); err != nil {
		t.Fatal(err)
	}
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		t.Fatal(err)
	}
	for _, dacl := range []string{"D:NO_ACCESS_CONTROL", "D:P(A;;FA;;;WD)", "D:P(A;;FA;;;BU)"} {
		sd, err := windows.SecurityDescriptorFromString("O:" + user.User.Sid.String() + dacl)
		if err != nil {
			t.Fatal(err)
		}
		if checkPrivateSecurity(sd) == nil {
			t.Fatalf("unsafe ACL accepted: %s", dacl)
		}
	}
}
