-- +goose Up
-- Device detail looks up a device's active rollout; package list counts per package.
CREATE INDEX rollout_devices_sn ON rollout_devices(sn);
CREATE INDEX device_packages_name ON device_packages(name, version);

-- +goose Down
DROP INDEX device_packages_name;
DROP INDEX rollout_devices_sn;
