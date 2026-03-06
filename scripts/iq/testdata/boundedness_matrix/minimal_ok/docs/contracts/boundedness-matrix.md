# Fixture Boundedness Matrix

<!-- boundedness-matrix:data:start -->
```json
{
  "catalog_version": "fixture-minimal-ok",
  "entries": [
    {
      "id": "backend.delivery.session_outbound_queue_size",
      "subsystem": "backend.delivery",
      "structure": "ws_session_outbound_queue",
      "cap": 512,
      "unit": "entries",
      "default": "fixture",
      "override": "fixture",
      "metric": "ws_queue_capacity",
      "anchors": [
        {
          "file": "internal/shared/config/loader.go",
          "line": 3,
          "snippet": "c.Delivery.SessionOutboundQueueSize = 512"
        }
      ]
    }
  ]
}

```
<!-- boundedness-matrix:data:end -->
