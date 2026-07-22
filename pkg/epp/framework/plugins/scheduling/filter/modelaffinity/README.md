# Model Affinity Filter

**Type:** `model-affinity-filter`

**Interface:** `scheduling.Filter`

Retains only candidate endpoints whose configured label value matches the
request's target model. Designed for multi-cluster hub deployments where
endpoints are discovered via file-discovery and each endpoint entry is labelled
with the model it serves.

---

## What It Does

In a hub EPP topology, the EPP routes inference requests to downstream clusters
(spokes). Each spoke typically serves a specific model. By labelling file-discovery
entries with their served model, this filter ensures that only spokes serving the
requested model are considered as candidates — enabling model-aware routing
without requiring per-model scheduler profiles.

## Inputs Consumed

- **Request header** (configurable, default: `x-gateway-model-name`) — set by
  IPP's `body-field-to-header` plugin in the ext_proc chain before EPP.
- **Fallback: `InferenceRequest.TargetModel`** — resolved from the body's
  `model` field or the `x-llm-d-model-name-rewrite` header by the EPP director.
- Endpoint label matching the configured `labelKey` (default: `model`).

## Configuration

### Parameters

| Name | Type | Required | Default | Description |
|------|------|----------|---------|-------------|
| `labelKey` | `string` | No | `model` | The endpoint label key whose value is compared against the resolved model name. |
| `modelHeader` | `string` | No | `x-gateway-model-name` | The request header from which the target model name is read. Set by IPP's `body-field-to-header` plugin. When not present, falls back to `TargetModel`. Set to empty string (`""`) to always use TargetModel. |

### Example

```yaml
plugins:
  - type: model-affinity-filter
    name: model-filter
    parameters:
      labelKey: model
      modelHeader: x-gateway-model-name
schedulingProfiles:
  - name: default
    plugins:
      - pluginRef: model-filter
```

### How It Integrates with IPP

In a multi-cluster Hub deployment, IPP and EPP run as chained ext_proc filters
on the same Envoy:

```
ext_proc #1 (IPP)                    ext_proc #2 (EPP)
┌──────────────────────┐             ┌──────────────────────┐
│ body-field-to-header │             │ model-affinity-filter│
│ extracts "model"     │────────────▶│ reads header         │
│ from body → sets     │  (Envoy     │ matches against      │
│ X-Gateway-Model-Name │  mutates    │ endpoint labels)     │
│ header               │  headers)   │                      │
└──────────────────────┘             └──────────────────────┘
```

IPP configuration (on Hub):
```yaml
plugins:
  - type: body-field-to-header
    parameters:
      fieldName: model
      headerName: X-Gateway-Model-Name
```

### Endpoints File (file-discovery)

```yaml
endpoints:
  - name: spoke1-llama
    address: "10.0.0.1"
    port: "8000"
    labels:
      model: llama-3-70b
  - name: spoke2-llama
    address: "10.0.0.2"
    port: "8000"
    labels:
      model: llama-3-70b
  - name: spoke3-mistral
    address: "10.0.0.3"
    port: "8000"
    labels:
      model: mistral-7b
```

With the above configuration, a request targeting `llama-3-70b` is routed only
to `spoke1-llama` and `spoke2-llama`; a request for `mistral-7b` is routed
only to `spoke3-mistral`.

## Behaviour Details

- **Model resolution order**: Header (`modelHeader`) → `TargetModel` (body/director).
- **No model resolved** (nil request, empty header, empty TargetModel): All
  endpoints pass through unfiltered.
- **Endpoints without the label**: Filtered out (they cannot match any model).
- **No matching endpoints**: Returns an empty list — the scheduler reports a
  routing error rather than sending to the wrong model server.

## Use Cases

- **Multi-cluster Hub EPP**: Route requests to the correct spoke cluster based
  on which model it serves.
- **Multi-model single cluster**: When a single cluster runs multiple model
  servers (e.g., via LoRA adapters), label endpoints to route to the correct
  server group.
- **Gradual rollouts**: Label a subset of endpoints with a new model version;
  configure traffic splitting at the InferenceModel level and use this filter
  to ensure each split targets the correct endpoints.

## Limitations

- Only exact string equality is checked against the label value — no wildcards,
  prefix matching, or regular expressions.
- Each endpoint can only carry a single value per label key. Endpoints serving
  multiple models require one entry per model (with different names but the same
  address).

## Related Documentation

- [File Discovery Plugin](../../datalayer/discovery/file/README.md)
- [Creating a Custom Filter](../../../../../../../docs/create_new_filter.md)
- [Label-Based Filter Plugins](../bylabel/README.md)
