CREATE TABLE IF NOT EXISTS json_example (
    id SERIAL PRIMARY KEY,
    json_data JSONB NOT NULL,
    int_value INT NOT NULL
);

INSERT INTO json_example (json_data, int_value) VALUES ('{"name": "John", "age": 30}', 1);
INSERT INTO json_example (json_data, int_value) VALUES ('{"name": "Jane", "age": 25}', 2);
INSERT INTO json_example (json_data, int_value) VALUES ('{"name": "Jim", "age": 35}', 3);

ALTER PUBLICATION seatbelt_pub ADD TABLE json_example;