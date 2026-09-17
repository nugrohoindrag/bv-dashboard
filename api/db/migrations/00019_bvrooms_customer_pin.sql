-- +goose Up
-- BVRooms: login nomor HP + PIN 4 digit menggantikan OTP SMS selama vendor SMS di-hold (D3).
-- pin_hash NULL = masih memakai PIN default (BV_BVROOMS_DEFAULT_PIN, bawaan 1234) untuk akun lama; ganti PIN mengisi hash argon2id.
ALTER TABLE bvrooms_customers
  ADD COLUMN pin_hash          text,
  ADD COLUMN pin_failed        int NOT NULL DEFAULT 0,
  ADD COLUMN pin_locked_until  timestamptz;

-- +goose Down
ALTER TABLE bvrooms_customers DROP COLUMN pin_hash, DROP COLUMN pin_failed, DROP COLUMN pin_locked_until;
