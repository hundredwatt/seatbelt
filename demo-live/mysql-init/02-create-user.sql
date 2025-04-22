-- Create Debezium user with necessary replication permissions
CREATE USER 'replication_user'@'%' IDENTIFIED WITH mysql_native_password BY 'abc123';
GRANT SELECT, RELOAD, SHOW DATABASES, REPLICATION SLAVE, REPLICATION CLIENT ON *.* TO 'replication_user'@'%';
GRANT ALL PRIVILEGES ON mysql_db.* TO 'replication_user'@'%';
FLUSH PRIVILEGES; 
