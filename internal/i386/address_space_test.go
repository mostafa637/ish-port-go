package i386

import "testing"

func TestAddressSpaceMappingsAndHoles(t *testing.T) {
	a := NewAddressSpace(1 << 20)
	if _, err := a.MapAnonymous(0x10000, 0x2000, ProtRead|ProtWrite, "heap"); err != nil {
		t.Fatal(err)
	}
	if _, err := a.MapAnonymous(0x11000, 0x1000, ProtRead, "overlap"); err == nil {
		t.Fatal("expected overlap rejection")
	}
	hole, err := a.MapAnonymous(0, 0x3000, ProtRead|ProtWrite, "anon")
	if err != nil {
		t.Fatal(err)
	}
	if hole == 0x10000 {
		t.Fatalf("hole overlaps existing map: 0x%x", hole)
	}
	if got, ok := a.MappingAt(0x10010); !ok || got.Name != "heap" {
		t.Fatalf("MappingAt = (%+v,%t)", got, ok)
	}
}

func TestAddressSpacePartialUnmapAndProtect(t *testing.T) {
	a := NewAddressSpace(1 << 20)
	if err := a.Add(0x20000, 0x4000, ProtRead|ProtWrite, "test"); err != nil {
		t.Fatal(err)
	}
	if err := a.Remove(0x21000, 0x1000); err != nil {
		t.Fatal(err)
	}
	if _, ok := a.MappingAt(0x21000); ok {
		t.Fatal("unmapped middle page still reported mapped")
	}
	if _, ok := a.MappingAt(0x20000); !ok {
		t.Fatal("left mapping disappeared")
	}
	if err := a.Protect(0x22000, 0x1000, ProtRead); err != nil {
		t.Fatal(err)
	}
	m, ok := a.MappingAt(0x22000)
	if !ok || m.Prot != ProtRead {
		t.Fatalf("protected mapping = (%+v,%t)", m, ok)
	}
}

func TestAddressSpaceCloneIsIndependent(t *testing.T) {
	a := NewAddressSpace(1 << 20)
	if err := a.Add(0x30000, 0x1000, ProtRead|ProtWrite, "data"); err != nil {
		t.Fatal(err)
	}
	if err := a.Mem.Write32(0x30000, 7); err != nil {
		t.Fatal(err)
	}
	b := a.Clone()
	if err := b.Mem.Write32(0x30000, 9); err != nil {
		t.Fatal(err)
	}
	got, err := a.Mem.Read32(0x30000)
	if err != nil || got != 7 {
		t.Fatalf("original memory = (%d,%v)", got, err)
	}
}
