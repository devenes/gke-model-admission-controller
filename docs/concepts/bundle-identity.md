# Concept: Deterministic Model Bundle Identity (`bundle-identity/v1`)

An AI model is rarely a single file. A typical model bundle comprises `config.json`, one or more `*.safetensors` weight shards, `tokenizer.json`, `tokenizer_config.json`, `special_tokens_map.json`, and `chat_template.jinja` (or PEFT/LoRA `adapter_config.json` and `adapter_model.safetensors`).

`bundle-identity/v1` (implemented in [`ai-model-provenance-gate`](https://github.com/devenes/ai-model-provenance-gate)) defines a deterministic, transport-independent cryptographic identity (`bundle_id`) over all regular files in a model bundle.

---

## 1. How `bundle_id` Is Computed

1. **Enumerate & Classify Files**: Every regular file in the bundle is enumerated by its normalized POSIX relative path (`path`), byte size (`size`), file role (`model_config`, `model_weights`, `tokenizer_vocab`, `lora_config`, `lora_weights`, `chat_template`), and raw SHA-256 digest (`sha256:<64-lowercase-hex>`). Symlinks and path traversal sequences (`..`) are rejected.
2. **Sort Lexicographically**: The file entries are sorted by `path`.
3. **Canonicalize via RFC 8785 (JCS)**: The inventory structure `{ "schema_version": "bundle-identity/v1", "files": [...] }` is serialized using RFC 8785 JSON Canonicalization Scheme (JCS).
4. **Hash**: `bundle_id = "sha256:" + hex(SHA256(jcsBytes))`.

---

## 2. Generating `bundle-manifest.json`

Using `ai-model-provenance inspect`:

```bash
ai-model-provenance inspect \
  --bundle ./examples/fixtures/minimal-safetensors \
  --output /tmp/inspect-out
```

Output:

```text
COMMAND=inspect GATE_STATUS=PASS BUNDLE_ID=sha256:4ea48db6aa00c7908197674f0818f2c8a09821246ed021c8488ce84873cae97a FILES=6 MANIFEST=/tmp/inspect-out/bundle-manifest.json JSON=/tmp/inspect-out/inspect-report.json MARKDOWN=/tmp/inspect-out/inspect-report.md
```

---

## 3. Why `bundle_id` Is Separate from the OCI Manifest Digest

- **OCI Manifest Digest (`@sha256:...`)** hashes the OCI Image Manifest JSON (which includes OCI mediaTypes and annotations).
- **`bundle_id` (`sha256:...`)** hashes the canonical file inventory (`bundle-identity/v1`).
- Because `bundle_id` is independent of OCI packaging metadata, `llm-release-gate` can bind an evaluation run to `bundle_id` *before* the model is pushed to an OCI registry, and `ModelArtifactReconciler` can later verify that the OCI layers in Google Artifact Registry recompute to the exact same `bundle_id`.
