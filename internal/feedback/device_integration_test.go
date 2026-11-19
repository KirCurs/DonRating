package feedback

import (
	"context"
	"errors"
	"testing"
)

func TestDeviceRegistryPersistsAcrossStores(t *testing.T) {
	store, pool := integrationStore(t)
	ctx := context.Background()
	device := RegisteredDevice{ID: "tablet-one", Name: "Left desk", TokenHash: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"}
	if err := store.RegisterDevice(ctx, device); err != nil {
		t.Fatal(err)
	}
	restarted := NewStore(pool)
	got, err := restarted.GetDevice(ctx, device.ID)
	if err != nil || got != device {
		t.Fatalf("reload: %+v %v", got, err)
	}
	devices, err := restarted.ListDevices(ctx)
	if err != nil || len(devices) != 1 || devices[0].TokenHash != "" {
		t.Fatalf("listing: %+v %v", devices, err)
	}
	if err := restarted.RegisterDevice(ctx, device); err == nil {
		t.Fatal("duplicate enrollment accepted")
	}
	if _, err := restarted.GetDevice(ctx, "unknown"); !errors.Is(err, ErrDeviceNotFound) {
		t.Fatal(err)
	}
	applyTestMigration(t, pool, "000004_device_registry.down.sql")
	applyTestMigration(t, pool, "000004_device_registry.up.sql")
}
