# Heartbeat Collector

## Usage

### Running the application
To run the heartbeat collector, use the following command in the root of the project

```sh
task run
```

### Creating a heartbeat

```sh
curl -X PUT http://localhost:8181/hb/{id}
```

### Checking an existing heartbeat

```sh
curl -X GET http://localhost:8080/hb/{id}?ttl={ttl_in_seconds}

{
    "id": "id",
    "last_updated_at": "2025-12-31T23:59:59Z"
}
```