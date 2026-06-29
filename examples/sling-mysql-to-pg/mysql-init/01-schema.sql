-- Source schema for the MySQL → Postgres (Sling) example.
-- The data itself is loaded from seed/products.csv by run.sh (LOAD DATA LOCAL INFILE),
-- so the CSV stays the single source of truth.

CREATE DATABASE IF NOT EXISTS shop;
USE shop;

CREATE TABLE IF NOT EXISTS products (
    id       INT PRIMARY KEY,
    sku      VARCHAR(32)  NOT NULL,
    name     VARCHAR(100) NOT NULL,
    category VARCHAR(50)  NOT NULL,
    quantity INT          NOT NULL
);
