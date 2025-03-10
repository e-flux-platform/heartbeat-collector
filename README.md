# Heartbeat Collector

## Usage

### Running the application
To run the heartbeat collector, use the following command in the root of the project

```sh
task run
```

### Creating a heartbeat

```sh
 curl -X PUT http://localhost:8181/hb/{id} -d '{
    "expiry": "2025-12-31T23:59:59Z",
    "label": "example",
    "metadata": {
        "k1": "v1",
        "k2": "v2"
    }
}' -H "Content-Type: application/json"
```

### Reading a heartbeat
```sh
 curl -X GET  http://localhost:8080/hb/{id}   

{
    "id":"id",
    "expiry":"2025-12-31T23:59:59Z",
    "label":"example-label",
    "metadata":
        {
            "k1":"1",
            "k2":"v2"
        }
    }
```