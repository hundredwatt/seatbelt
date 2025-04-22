# MySQL to PostgreSQL Replication Demo

This project demonstrates real-time data replication from MySQL to PostgreSQL using Debezium, Apache Kafka, and Kafka Connect.

## Components

- **MySQL**: Source database (runs on port 3306)
- **PostgreSQL**: Target database (runs on port 5432)
- **Apache Kafka**: Message broker for data streaming
- **Zookeeper**: Required for Kafka
- **Kafka Connect**: With Debezium (MySQL source) and JDBC (PostgreSQL sink) connectors

## How It Works

1. The MySQL database has CDC (Change Data Capture) enabled via binary logs
2. Debezium monitors the MySQL binary log for changes
3. Changes are published to Kafka topics
4. JDBC Sink Connector consumes from Kafka and writes to PostgreSQL

## Setup and Usage

### Start the Stack

```bash
# Bring up all services
docker-compose up -d

# Wait for all services to be healthy
# Then register the connectors
./register-connectors.sh
```

### Check Replication Status

```bash
# Check if data is being replicated
./check-replication.sh
```

### Test Replication

To test the replication, you can insert, update, or delete data in the MySQL database and see the changes replicated to PostgreSQL:

```bash
# Insert new data in MySQL
docker exec mysql_source mysql -u root -pdebezium -e "INSERT INTO inventory.demo_data (test_name, name, score) VALUES ('test_replication', 'New Row', 95.0);"

# Check if the data was replicated to PostgreSQL
docker exec postgres_sink psql -U postgres -c "SELECT * FROM demo_data WHERE test_name = 'test_replication';"
```

### Stopping the Stack

```bash
docker-compose down
```

## Data Model

The demo replicates a table called `demo_data` which contains various data types to test replication functionality:

- Strings
- Integers
- Floats
- Decimals
- Timestamps
- JSON data 