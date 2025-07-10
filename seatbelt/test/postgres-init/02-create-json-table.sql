CREATE TABLE IF NOT EXISTS json_example (
    id SERIAL PRIMARY KEY,
    json_data JSONB NOT NULL
);

INSERT INTO json_example (json_data) VALUES ('{"name": "John", "age": 30}');
INSERT INTO json_example (json_data) VALUES ('{"name": "Jane", "age": 25}');
INSERT INTO json_example (json_data) VALUES ('{"name": "Jim", "age": 35}');

ALTER PUBLICATION seatbelt_pub ADD TABLE json_example;