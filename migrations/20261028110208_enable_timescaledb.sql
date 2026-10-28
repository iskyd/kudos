-- +goose Up
CREATE EXTENSION IF NOT EXISTS timescaledb;

-- +goose Down
DROP EXTENSION IF EXISTS timescaledb;
