CREATE DATABASE IF NOT EXISTS mysql_db;
USE mysql_db;

-- Create a single table that includes all data types from the test cases
CREATE TABLE IF NOT EXISTS demo_data (
    id INT AUTO_INCREMENT PRIMARY KEY,
    name VARCHAR(100),
    score FLOAT,
    price DECIMAL(10,2),
    temperature INTEGER,
    timestamp TIMESTAMP NULL,
    data JSON,
    text VARCHAR(255),
    test_name VARCHAR(100)
);
TRUNCATE demo_data;

-- Basic validation tests
INSERT INTO demo_data (test_name, name, score) VALUES
-- corrupt_target.yaml
('corrupt_target', 'Row 1', 85.5),
('corrupt_target', 'Row 2', 92.3),

-- discrepant_delete.yaml
('discrepant_delete', 'Row 1', 75.0),
('discrepant_delete', 'Row 2', 82.5),

-- discrepant_insert.yaml
('discrepant_insert', 'Row 1', 65.5),

-- discrepant_update.yaml
('discrepant_update', 'Row 1', 70.0),
('discrepant_update', 'Row 2', 80.0),

-- valid_delete.yaml
('valid_delete', 'Row 1', 77.7),
('valid_delete', 'Row 2', 88.8),

-- valid_insert.yaml
('valid_insert', 'Row 1', 90.0),

-- valid_update.yaml
('valid_update', 'Row 1', 60.0),
('valid_update', 'Row 2', 75.0);

-- DateTime test data
INSERT INTO demo_data (test_name, timestamp) VALUES
-- discrepant_datetime.yaml
('discrepant_datetime', NULL),
('discrepant_datetime', '2023-03-12 01:30:00'),
('discrepant_datetime', '2024-02-29 16:59:59'),
('discrepant_datetime', '2038-01-18 20:14:07'),
('discrepant_datetime', '2038-01-19 03:14:07'),
('discrepant_datetime', '1970-01-01 00:00:01'),

-- valid_datetime.yaml
('valid_datetime', NULL),
('valid_datetime', '1970-01-01 00:00:01'),
('valid_datetime', '2038-01-19 03:14:07'),
('valid_datetime', '2038-01-18 20:14:07'),
('valid_datetime', '2024-02-29 16:59:59'),
('valid_datetime', '2023-03-12 01:30:00');

-- Decimal test data
INSERT INTO demo_data (test_name, price) VALUES
-- discrepant_decimal.yaml
('discrepant_decimal', NULL),
('discrepant_decimal', 0.00),
('discrepant_decimal', -0.00),
('discrepant_decimal', 1.00),
('discrepant_decimal', -1.00),
('discrepant_decimal', 9999999.99),
('discrepant_decimal', -9999999.99),
('discrepant_decimal', 0.01),
('discrepant_decimal', -0.01),

-- valid_decimal.yaml
('valid_decimal', NULL),
('valid_decimal', 0.00),
('valid_decimal', -0.00),
('valid_decimal', 1.00),
('valid_decimal', -1.00),
('valid_decimal', 9999999.99),
('valid_decimal', -9999999.99),
('valid_decimal', 0.01),
('valid_decimal', -0.01);

-- Integer test data
INSERT INTO demo_data (test_name, temperature) VALUES
-- discrepant_integer.yaml
('discrepant_integer', NULL),
('discrepant_integer', 0),
('discrepant_integer', 1),
('discrepant_integer', -1),
('discrepant_integer', 2147483647),
('discrepant_integer', -2147483648),

-- valid_integer.yaml
('valid_integer', NULL),
('valid_integer', 0),
('valid_integer', 1),
('valid_integer', -1),
('valid_integer', 2147483647),
('valid_integer', -2147483648);

-- Float test data
INSERT INTO demo_data (test_name, score) VALUES
-- discrepant_float.yaml
('discrepant_float', NULL),
('discrepant_float', 0.0),
('discrepant_float', -0.0),
('discrepant_float', 1.0),
('discrepant_float', -1.0),
('discrepant_float', 3.40282e+38),
('discrepant_float', 1.17549e-38),
('discrepant_float', '3.40282e+38'),
('discrepant_float', '-3.40282e+38'),

-- valid_float.yaml
('valid_float', NULL),
('valid_float', 0.0),
('valid_float', -0.0),
('valid_float', 1.0),
('valid_float', -1.0),
('valid_float', 3.40282e+38),
('valid_float', 1.17549e-38),
('valid_float', '3.40282e+38'),
('valid_float', '-3.40282e+38');

-- String test data
INSERT INTO demo_data (test_name, text) VALUES
-- discrepant_string.yaml
('discrepant_string', NULL),
('discrepant_string', ''),
('discrepant_string', 'a'),
('discrepant_string', 'abcdefghijklmnopqrstuvwxyz'),
('discrepant_string', '😀'),

-- valid_string.yaml
('valid_string', NULL),
('valid_string', ''),
('valid_string', 'a'),
('valid_string', 'abcdefghijklmnopqrstuvwxyz'),
('valid_string', '😀');

-- JSON test data
INSERT INTO demo_data (test_name, data) VALUES
-- discrepant_json.yaml
('discrepant_json', NULL),
('discrepant_json', '[]'),
('discrepant_json', '{}'),
('discrepant_json', '{"a": 1, "b": 2, "c": 3}'),
('discrepant_json', '["a", 1, "b", 2, "c", 3]'),
('discrepant_json', '{"nested": {"x": 1, "y": 2}, "list": [1, 2, 3]}'),

-- valid_json.yaml
('valid_json', NULL),
('valid_json', '[]'),
('valid_json', '{}'),
('valid_json', '{"a": 1, "b": 2, "c": 3}'),
('valid_json', '["a", 1, "b", 2, "c", 3]'),
('valid_json', '{"nested": {"x": 1, "y": 2}, "list": [1, 2, 3]}'),
('valid_json', '{"a": 1, "b": 2, "c": 3}'); 
