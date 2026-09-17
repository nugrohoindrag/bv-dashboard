-- +goose Up
-- App Downloads: tipe ketiga "customer" (BVRooms Customer Booking App) di samping staff & tenant (Website PRD §18).
ALTER TABLE app_downloads DROP CONSTRAINT IF EXISTS app_downloads_app_type_check;
ALTER TABLE app_downloads ADD CONSTRAINT app_downloads_app_type_check CHECK (app_type IN ('staff','tenant','customer'));

-- +goose Down
DELETE FROM app_downloads WHERE app_type = 'customer';
ALTER TABLE app_downloads DROP CONSTRAINT IF EXISTS app_downloads_app_type_check;
ALTER TABLE app_downloads ADD CONSTRAINT app_downloads_app_type_check CHECK (app_type IN ('staff','tenant'));
