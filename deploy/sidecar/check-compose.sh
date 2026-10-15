#!/usr/bin/env bash
# Render only: never pulls images, starts services or downloads weights.
set -euo pipefail
cd "$(dirname "$0")/../.."
export BOYAN_IMAGE="${BOYAN_IMAGE:-boyan-local:validation}"
docker compose -f deploy/sidecar/compose.yaml config --format json | jq -e '
  (.services | keys == ["boyan"]) and
  (.services.boyan.depends_on == null) and
  (.services.boyan.environment.BOYAN_EMBEDDINGS_ENABLED == "false") and
  (.services.boyan.ports == null) and
  (.services.boyan.environment.BOYAN_HTTP_LISTEN == "127.0.0.1:8686")
' >/dev/null
docker compose -f deploy/sidecar/compose.yaml \
  -f deploy/sidecar/embeddings.compose.yaml config --format json | jq -e '
  .services.boyan as $b | .services.tei as $t |
  def option($name): $t.command as $c | $c[($c | index($name)) + 1];
  ($b.depends_on == null) and
  ($b.environment.BOYAN_EMBEDDINGS_ENABLED == "true") and
  ($b.environment.BOYAN_EMBEDDINGS_ENDPOINT == "http://127.0.0.1:8080/v1/embeddings") and
  ($b.environment.BOYAN_EMBEDDINGS_DIMENSIONS == "768") and
  ($b.environment.BOYAN_EMBEDDINGS_QUERY_PREFIX == "query: ") and
  ($b.environment.BOYAN_EMBEDDINGS_PASSAGE_PREFIX == "passage: ") and
  ($b.environment.BOYAN_EMBEDDINGS_FAILURE_POLICY == "lexical") and
  ($b.environment.BOYAN_EMBEDDINGS_API_KEY_ENV == "") and
  ($t.image == "ghcr.io/huggingface/text-embeddings-inference:cpu-1.9.0@sha256:bc7ad262695df5b7875b0c9c702deb8e9df3953bdf22c3d6068d9c0429b7b3f3") and
  ($t.platform == "linux/amd64") and ($t.cpus == 2) and ($t.mem_limit == "4294967296") and
  ($t.network_mode == "service:boyan") and ($t.ports == null) and ($t.networks == null) and
  ($b.ports == null) and (.networks["boyan-private"].driver == "bridge") and
  ($t.environment.AUTO_TRUNCATE == "false") and
  ($t.environment.RAYON_NUM_THREADS == "2") and
  ($t.environment.OMP_NUM_THREADS == "2") and ($t.environment.MKL_NUM_THREADS == "2") and
  (option("--model-id") == $b.environment.BOYAN_EMBEDDINGS_MODEL) and
  (option("--served-model-name") == "intfloat/multilingual-e5-base") and
  (option("--revision") == $b.environment.BOYAN_EMBEDDINGS_REVISION) and
  (option("--revision") == "d128750597153bb5987e10b1c3493a34e5a4502a") and
  (option("--hostname") == "127.0.0.1") and (option("--port") == "8080") and
  (option("--dtype") == "float32") and (option("--pooling") == "mean") and
  (option("--tokenization-workers") == "2") and
  (option("--max-concurrent-requests") == "4") and
  (option("--max-client-batch-size") == "16") and
  (option("--max-batch-requests") == "8") and
  (option("--max-batch-tokens") == "8192") and (option("--payload-limit") == "65536") and
  (option("--huggingface-hub-cache") == "/data") and
  ($t.volumes | any(.type == "volume" and .source == "tei-model-cache" and .target == "/data")) and
  ($t.healthcheck.test == ["CMD", "curl", "--fail", "--silent", "http://127.0.0.1:8080/health"]) and
  ($t.healthcheck.interval == "10s") and ($t.healthcheck.timeout == "3s") and
  ($t.healthcheck.retries == 12) and ($t.healthcheck.start_period == "10m0s")
' >/dev/null
printf '%s\n' 'Default and optional TEI Compose contracts passed (render only).'
