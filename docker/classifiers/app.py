"""
Text classification inference server.

Supports standard text-classification and zero-shot NLI pipelines.

Endpoints:
  POST /classify     { "input": ["text1", "text2"], "model": "model-id" }
  POST /zero-shot    { "input": ["text1"], "model": "model-id", "candidate_labels": ["a", "b"] }
  Response:          { "classify": [[{"label": "...", "score": 0.9}, ...], ...] }

Features:
  - Microbatching: collects requests for BATCH_TIMEOUT_MS, then runs one batched
    model call - reduces overhead at moderate/high load while adding minimal latency
    at low load. (text-classification only; zero-shot runs synchronously.)
  - In-memory result cache: SHA-256(model_id + text) -> label-score list.
    Hits skip the batcher entirely. Unbounded; restart to clear.
  - Configurable model list: set CLASSIFIER_MODELS to a comma-separated list of
    HuggingFace model IDs. All models are loaded on worker startup.
  - Zero-shot models: set CLASSIFIER_ZERO_SHOT_MODELS to a comma-separated list of
    HuggingFace NLI model IDs. Uses zero-shot-classification pipeline with
    multi_label=True - each label is scored independently (sigmoid, not softmax),
    so threshold has absolute meaning regardless of how many labels are provided.
  - Gated models: set HF_TOKEN for models that require HuggingFace authentication
    (e.g. meta-llama/Llama-Prompt-Guard-2-86M). Token is only used on first download;
    cached models load without network access.

Tested Models Catalog (all verified across Swiftward demos):

  Standard classifiers (CLASSIFIER_MODELS, text-classification pipeline):
  -----------------------------------------------------------------------
  | Model ID                                           | Task              | Lang   | Labels / Notes                                     | Used in             |
  |------------------------------------------------------|-------------------|--------|------------------------------------------------------|---------------------|
  | meta-llama/Llama-Prompt-Guard-2-86M                | Prompt injection  | Multi  | BENIGN, INJECTION, LABEL_1 (gated, needs HF_TOKEN)  | enterprise, llm     |
  | devndeploy/bert-prompt-injection-detector           | Prompt injection  | EN     | SAFE, INJECTION                                      | enterprise, llm     |
  | unitary/toxic-bert                                  | Toxicity          | EN     | toxic, severe_toxic, obscene, threat, insult, identity_hate | enterprise, ugc |
  | cointegrated/rubert-tiny-toxicity                   | Toxicity          | RU     | non-toxic, insult, obscenity, threat, dangerous      | enterprise-moex, llm |
  | cardiffnlp/twitter-roberta-base-sentiment-latest    | Sentiment         | EN     | positive, neutral, negative                          | enterprise          |
  | seara/rubert-tiny2-russian-sentiment                | Sentiment         | RU     | positive, neutral, negative                          | enterprise-moex, llm |
  | apanc/russian-sensitive-topics                      | Content safety    | RU     | finance, politics, religion, etc.                    | enterprise-moex, llm |
  | KoalaAI/Text-Moderation                             | Content moderation| EN     | S (sexual), H (hate), V (violence), HR, SH, S3, H2  | enterprise          |
  | cardiffnlp/twitter-roberta-base-hate-latest         | Hate speech       | EN     | NOT-HATE, HATE                                       | ugc                 |
  | eliasalbouzidi/distilbert-nsfw-text-classifier      | NSFW              | EN     | nsfw, sfw                                            | ugc                 |

  Zero-shot NLI classifiers (CLASSIFIER_ZERO_SHOT_MODELS, zero-shot-classification pipeline):
  --------------------------------------------------------------------------------------------
  | Model ID                                           | Task              | Lang   | Notes                                                |
  |------------------------------------------------------|-------------------|--------|------------------------------------------------------|
  | MoritzLaurer/mDeBERTa-v3-base-xnli-multilingual-nli-2mil7 | Zero-shot NLI | Multi | Arbitrary labels, multi_label=True (sigmoid scoring) |
"""

import hashlib
import os
import queue
import threading
import time

from flask import Flask, jsonify, request
from transformers import AutoModelForSequenceClassification, AutoTokenizer, pipeline

app = Flask(__name__)

# -- Config --------------------------------------------------------------------

MODEL_IDS: list[str] = [
    m.strip()
    for m in os.environ.get("CLASSIFIER_MODELS", "").split(",")
    if m.strip()
]
ZERO_SHOT_MODEL_IDS: list[str] = [
    m.strip()
    for m in os.environ.get("CLASSIFIER_ZERO_SHOT_MODELS", "").split(",")
    if m.strip()
]
BATCH_MAX_SIZE: int = int(os.environ.get("BATCH_MAX_SIZE", "32"))
BATCH_TIMEOUT_MS: float = float(os.environ.get("BATCH_TIMEOUT_MS", "5"))
HF_TOKEN: str | None = os.environ.get("HF_TOKEN") or None

# -- State ---------------------------------------------------------------------

_pipelines: dict[str, object] = {}       # model_id -> transformers pipeline
_queues: dict[str, queue.Queue] = {}     # model_id -> batch queue
_cache: dict[tuple, list] = {}           # (model_id, sha256) -> label-score list
_cache_lock = threading.Lock()

_zs_pipelines: dict[str, object] = {}   # model_id -> zero-shot pipeline
_zs_cache: dict[tuple, list] = {}       # (model_id, sha256, labels_key) -> label-score list
_zs_cache_lock = threading.Lock()

# -- Cache ---------------------------------------------------------------------


def _cache_key(model_id: str, text: str) -> tuple:
    return (model_id, hashlib.sha256(text.encode()).hexdigest())


def _cache_get(model_id: str, text: str) -> list | None:
    return _cache.get(_cache_key(model_id, text))


def _cache_put(model_id: str, text: str, scores: list) -> None:
    with _cache_lock:
        _cache[_cache_key(model_id, text)] = scores


def _zs_cache_key(model_id: str, text: str, labels_key: str) -> tuple:
    return (model_id, hashlib.sha256(text.encode()).hexdigest(), labels_key)


def _zs_cache_get(model_id: str, text: str, labels_key: str) -> list | None:
    return _zs_cache.get(_zs_cache_key(model_id, text, labels_key))


def _zs_cache_put(model_id: str, text: str, labels_key: str, scores: list) -> None:
    with _zs_cache_lock:
        _zs_cache[_zs_cache_key(model_id, text, labels_key)] = scores


# -- Microbatcher --------------------------------------------------------------


class _Item:
    """One classify call, potentially covering multiple input texts."""

    __slots__ = ("texts", "results", "error", "done")

    def __init__(self, texts: list[str]) -> None:
        self.texts = texts
        self.results: list | None = None
        self.error: str | None = None
        self.done = threading.Event()


def _run_batch(model_id: str, pipe, items: list) -> None:
    """Run one batched text-classification inference call across all items."""
    flat: list[str] = []
    offsets: list[int] = []
    for item in items:
        offsets.append(len(flat))
        flat.extend(item.texts)

    try:
        flat_out = pipe(
            flat,
            top_k=None,
            truncation=True,
            max_length=512,
            batch_size=len(flat),
        )

        for i, item in enumerate(items):
            start = offsets[i]
            end = offsets[i + 1] if i + 1 < len(offsets) else len(flat_out)
            item.results = flat_out[start:end]
            item.done.set()

            for j, text in enumerate(item.texts):
                _cache_put(model_id, text, flat_out[start + j])

    except Exception as exc:  # noqa: BLE001
        err = str(exc)
        for item in items:
            item.error = err
            item.done.set()


def _batcher_loop(model_id: str, q: queue.Queue) -> None:
    """Daemon thread: drains the queue into batches and runs inference."""
    pipe = _pipelines[model_id]
    timeout_s = BATCH_TIMEOUT_MS / 1000.0

    while True:
        # Block until the first request arrives.
        try:
            first: _Item = q.get(timeout=1.0)
        except queue.Empty:
            continue

        items: list[_Item] = [first]
        deadline = time.monotonic() + timeout_s

        # Accumulate more requests until the window closes or batch is full.
        while len(items) < BATCH_MAX_SIZE:
            remaining = deadline - time.monotonic()
            if remaining <= 0:
                break
            try:
                items.append(q.get(timeout=remaining))
            except queue.Empty:
                break

        _run_batch(model_id, pipe, items)


# -- Startup -------------------------------------------------------------------


def _load_pipeline(model_id: str) -> object:
    """Load a model pipeline, preferring the local cache to avoid a network
    staleness check on startup.

    HuggingFace Hub always sends a HEAD request to check for model updates, even
    when the model is fully cached. On container startup the Docker network interface
    may not be ready yet, which causes a harmless but noisy ENETUNREACH warning.

    Strategy: load tokenizer and model separately with local_files_only=True first
    (silent, no network). If not cached, fall back to normal download.
    Keeping local_files_only out of pipeline() itself prevents it from leaking
    into tokenizer inference calls and causing unexpected keyword argument errors.
    """
    try:
        tokenizer = AutoTokenizer.from_pretrained(model_id, local_files_only=True)
        model = AutoModelForSequenceClassification.from_pretrained(model_id, local_files_only=True)
    except Exception:
        # Model not in cache - download it (first run). Pass token for gated models.
        tokenizer = AutoTokenizer.from_pretrained(model_id, token=HF_TOKEN)
        model = AutoModelForSequenceClassification.from_pretrained(model_id, token=HF_TOKEN)

    return pipeline(
        "text-classification",
        model=model,
        tokenizer=tokenizer,
        top_k=None,
        device=-1,       # CPU
        truncation=True,
        max_length=512,
    )


def _load_models() -> None:
    """Load all configured models and start one batcher thread per model.

    Called at module import time - runs inside the gunicorn worker process
    (no --preload), so threads are never forked and state is not shared across
    workers. Requires --workers 1 in gunicorn.
    """
    if not MODEL_IDS:
        print("WARNING: CLASSIFIER_MODELS is empty - no models loaded", flush=True)
        return

    for model_id in MODEL_IDS:
        print(f"Loading {model_id} ...", flush=True)
        t0 = time.monotonic()

        _pipelines[model_id] = _load_pipeline(model_id)

        q: queue.Queue = queue.Queue()
        _queues[model_id] = q
        threading.Thread(
            target=_batcher_loop, args=(model_id, q), daemon=True
        ).start()

        print(f"Ready  {model_id}  ({time.monotonic() - t0:.1f}s)", flush=True)


def _load_zs_pipeline(model_id: str) -> object:
    """Load a zero-shot-classification pipeline, preferring the local cache."""
    try:
        tokenizer = AutoTokenizer.from_pretrained(model_id, local_files_only=True)
        model = AutoModelForSequenceClassification.from_pretrained(model_id, local_files_only=True)
    except Exception:
        tokenizer = AutoTokenizer.from_pretrained(model_id, token=HF_TOKEN)
        model = AutoModelForSequenceClassification.from_pretrained(model_id, token=HF_TOKEN)

    return pipeline(
        "zero-shot-classification",
        model=model,
        tokenizer=tokenizer,
        device=-1,  # CPU
    )


def _load_zero_shot_models() -> None:
    """Load all configured zero-shot NLI models on worker startup."""
    if not ZERO_SHOT_MODEL_IDS:
        return

    for model_id in ZERO_SHOT_MODEL_IDS:
        print(f"Loading zero-shot {model_id} ...", flush=True)
        t0 = time.monotonic()
        _zs_pipelines[model_id] = _load_zs_pipeline(model_id)
        print(f"Ready  {model_id}  ({time.monotonic() - t0:.1f}s)", flush=True)


_load_models()
_load_zero_shot_models()

_total = len(_pipelines) + len(_zs_pipelines)
if _total:
    print(f"\n{'='*60}", flush=True)
    print(f"All {_total} classifier(s) downloaded and ready.", flush=True)
    print(f"{'='*60}\n", flush=True)

# -- Endpoints -----------------------------------------------------------------


@app.get("/health")
def health():
    return jsonify({
        "status": "ok",
        "models": list(_pipelines.keys()),
        "zero_shot_models": list(_zs_pipelines.keys()),
    })


@app.post("/classify")
def classify():
    body = request.get_json(silent=True) or {}
    model_id: str = body.get("model", "")
    texts: list = body.get("input", [])

    if model_id not in _pipelines:
        return jsonify(
            {"error": f"unknown model '{model_id}'", "available": list(_pipelines.keys())}
        ), 400

    if not isinstance(texts, list) or not texts:
        return jsonify({"error": "'input' must be a non-empty list"}), 400

    # Resolve cache hits; collect misses for batched inference.
    out: list = [None] * len(texts)
    miss_idx: list[int] = []
    miss_texts: list[str] = []

    for i, text in enumerate(texts):
        hit = _cache_get(model_id, text)
        if hit is not None:
            out[i] = hit
        else:
            miss_idx.append(i)
            miss_texts.append(text)

    # Send cache misses through the microbatcher.
    if miss_texts:
        item = _Item(miss_texts)
        _queues[model_id].put(item)

        if not item.done.wait(timeout=30.0):
            return jsonify({"error": "inference timeout"}), 504

        if item.error:
            return jsonify({"error": item.error}), 500

        for j, idx in enumerate(miss_idx):
            out[idx] = item.results[j]

    return jsonify({"classify": out})


@app.post("/zero-shot")
def zero_shot():
    body = request.get_json(silent=True) or {}
    model_id: str = body.get("model", "")
    texts: list = body.get("input", [])
    candidate_labels: list = body.get("candidate_labels", [])

    if model_id not in _zs_pipelines:
        return jsonify(
            {"error": f"unknown model '{model_id}'", "available": list(_zs_pipelines.keys())}
        ), 400

    if not isinstance(texts, list) or not texts:
        return jsonify({"error": "'input' must be a non-empty list"}), 400

    if not isinstance(candidate_labels, list) or not candidate_labels:
        return jsonify({"error": "'candidate_labels' must be a non-empty list"}), 400

    # Sort labels for a stable cache key regardless of input order.
    labels_key = ",".join(sorted(candidate_labels))
    pipe = _zs_pipelines[model_id]
    out: list = []

    for text in texts:
        cached = _zs_cache_get(model_id, text, labels_key)
        if cached is not None:
            out.append(cached)
            continue

        result = pipe(
            text,
            candidate_labels=candidate_labels,
            multi_label=True,
            truncation=True,
            max_length=512,
        )
        scores = [
            {"label": lbl, "score": scr}
            for lbl, scr in zip(result["labels"], result["scores"])
        ]
        _zs_cache_put(model_id, text, labels_key, scores)
        out.append(scores)

    return jsonify({"classify": out})
