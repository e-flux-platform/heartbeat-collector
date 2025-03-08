# Heartbeat Collector

## Usage

### Running the application
To run the heartbeat collector, use the following command in the root of the project

```sh
task watch
```

### Creating a heartbeat

```sh
 curl -X PUT http://localhost:8080/hb/secret -d '{
    "secret": "secret",
    "expiry": "2025-12-31T23:59:59Z",
    "label": "example",
    "metadata": {
        "k1": "v1",
        "k2": "v2"
    }
}' -H "Content-Type: application/json"

Heartbeat registered
```

### Reading a heartbeat
```sh
 curl -X GET  http://localhost:8081/hb/secret   

{
    "secret":"secret",
    "expiry":"2025-12-31T23:59:59Z",
    "label":"example-label",
    "metadata":
        {
            "k1":"1",
            "k2":"v2"
        }
    }
```