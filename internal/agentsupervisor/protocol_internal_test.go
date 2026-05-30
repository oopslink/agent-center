package agentsupervisor

import "testing"

// TestCompatibleInRange_AdditiveVsBreaking proves the (4a) compatibility STRATEGY
// is a RANGE, not exact-match — the whole point of survival across deploys. An
// additive ProtocolVersion bump (max grows, MinSupported stays) must keep older
// supervisors COMPATIBLE (re-attach survives); only a breaking bump of
// MinSupported drops the old versions (→ mode-B). Tested over a SYNTHETIC range
// because today the real range is the single point [1,1].
func TestCompatibleInRange_AdditiveVsBreaking(t *testing.T) {
	// Pretend 3 versions exist and v2/v3 were ADDITIVE (backward-compatible):
	// range = [1,3]. An upgraded daemon (speaks up to 3) re-attaching to an
	// older supervisor at 1 or 2 must still be COMPATIBLE = survive.
	for _, v := range []int{1, 2, 3} {
		if !compatibleInRange(v, 1, 3) {
			t.Fatalf("additive: version %d must be compatible in range [1,3] (survival preserved)", v)
		}
	}
	if compatibleInRange(4, 1, 3) {
		t.Fatal("version 4 (newer than this build understands) must be incompatible")
	}
	if compatibleInRange(0, 1, 3) {
		t.Fatal("version 0 (below floor) must be incompatible")
	}

	// Now a BREAKING change bumped MinSupported to 2 (v1 reframed/dropped):
	// range = [2,3]. Supervisors at v1 are now INCOMPATIBLE → s3 mode-B; v2/v3
	// still compatible.
	if compatibleInRange(1, 2, 3) {
		t.Fatal("breaking: v1 must be INCOMPATIBLE once MinSupported moved to 2 (→ mode-B)")
	}
	for _, v := range []int{2, 3} {
		if !compatibleInRange(v, 2, 3) {
			t.Fatalf("breaking: version %d must stay compatible in range [2,3]", v)
		}
	}
}

// TestIsCompatible_UsesRange guards that IsCompatible is wired to the
// [MinSupportedProtocol, ProtocolVersion] range (not an equality regression).
func TestIsCompatible_UsesRange(t *testing.T) {
	if !IsCompatible(MinSupportedProtocol) || !IsCompatible(ProtocolVersion) {
		t.Fatal("both ends of the supported range must be compatible")
	}
	if IsCompatible(MinSupportedProtocol - 1) {
		t.Fatal("below MinSupportedProtocol must be incompatible")
	}
	if IsCompatible(ProtocolVersion + 1) {
		t.Fatal("above ProtocolVersion must be incompatible")
	}
}
