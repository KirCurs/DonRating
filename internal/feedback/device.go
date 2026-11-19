package feedback

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
)

var ErrDeviceNotFound = errors.New("device not found")

type RegisteredDevice struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	TokenHash string `json:"-"`
}

func (store *Store) RegisterDevice(ctx context.Context, device RegisteredDevice) error {
	_, err := store.db.Exec(ctx, "INSERT INTO device_registry(id, name, token_hash) VALUES ($1,$2,$3)", device.ID, device.Name, device.TokenHash)
	return err
}
func (store *Store) ListDevices(ctx context.Context) ([]RegisteredDevice, error) {
	rows, err := store.db.Query(ctx, "SELECT id, name FROM device_registry ORDER BY created_at, id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	devices := []RegisteredDevice{}
	for rows.Next() {
		var d RegisteredDevice
		if err := rows.Scan(&d.ID, &d.Name); err != nil {
			return nil, err
		}
		devices = append(devices, d)
	}
	return devices, rows.Err()
}
func (store *Store) GetDevice(ctx context.Context, id string) (RegisteredDevice, error) {
	var d RegisteredDevice
	err := store.db.QueryRow(ctx, "SELECT id, name, token_hash FROM device_registry WHERE id=$1", id).Scan(&d.ID, &d.Name, &d.TokenHash)
	if errors.Is(err, pgx.ErrNoRows) {
		return RegisteredDevice{}, ErrDeviceNotFound
	}
	return d, err
}
