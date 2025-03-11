# Heartbeat Collector

## Usage

### Running the application
To run the heartbeat collector, use the following command in the root of the project

```sh
task run
```

### Creating a heartbeat

```sh
 curl -X GET  http://localhost:8080/{id}?expiry=<NUM_SECONDS>?label=label   

{
    "id":"id",
    "expiry":"2025-12-31T23:59:59Z",
    "label":"label",
    }
```

### Reading a heartbeat
```sh
 curl -X GET  http://localhost:8080/{id}   

{
    "id":"id",
    "expiry":"2025-12-31T23:59:59Z",
    "label":"label",
    }
```