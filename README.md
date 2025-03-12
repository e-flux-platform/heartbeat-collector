# Heartbeat Collector

## Usage

### Running the application
To run the heartbeat collector, use the following command in the root of the project

```sh
task run
```

### Creating a heartbeat

```sh
curl -X PUT http://localhost:8181/{id}
```

### Checking an existing heartbeat
Note the ttl query parameter should be specified as a duration (e.g. 1d, 2h, 30s, etc..)

```sh
curl -X GET http://localhost:8080/{id}?ttl={duration}

{
    "id": "id",
    "last_updated_at": "2025-12-31T23:59:59Z"
}
```
