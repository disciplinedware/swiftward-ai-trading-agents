# Text Classification Inference Server
#
# Infinity-compatible /classify API with microbatching + in-memory cache.
# Models loaded from CLASSIFIER_MODELS env var (comma-separated HuggingFace IDs).
#
# Multi-arch: linux/amd64 + linux/arm64 (Apple M-series and ARM servers)
#   amd64: CPU-only torch from PyTorch whl index (~300 MB vs ~2 GB CUDA build)
#   arm64: torch from PyPI (no CUDA wheels exist - always CPU-only)

FROM python:3.12-slim

WORKDIR /app

# Platform-specific torch install to keep the image small on amd64.
ARG TARGETPLATFORM
RUN if [ "$TARGETPLATFORM" = "linux/arm64" ]; then \
      pip install --no-cache-dir torch==2.5.1; \
    else \
      pip install --no-cache-dir torch==2.5.1 --index-url https://download.pytorch.org/whl/cpu; \
    fi

RUN pip install --no-cache-dir \
    transformers==4.47.0 \
    flask==3.1.0 \
    gunicorn==23.0.0

COPY app.py .

# Comma-separated HuggingFace model IDs - all loaded on startup.
# Example: seara/rubert-tiny2-russian-sentiment,cointegrated/rubert-tiny-toxicity
ENV CLASSIFIER_MODELS=""

# Microbatch window (ms) and max batch size.
ENV BATCH_TIMEOUT_MS=5
ENV BATCH_MAX_SIZE=32

ENV PYTHONUNBUFFERED=1
ENV PORT=3000
EXPOSE $PORT

# Single worker required: microbatcher uses shared in-process queues and cache.
# gthread provides multi-threaded concurrency within the one worker process.
# No --preload: models load in the worker (not the master) so threads survive.
CMD gunicorn \
    --workers 1 \
    --worker-class gthread \
    --threads 8 \
    --timeout 300 \
    --bind 0.0.0.0:${PORT} \
    app:app
