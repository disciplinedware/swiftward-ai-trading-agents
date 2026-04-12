-- Swiftward seed data for AI Trading Agents platform.
-- Idempotent upserts - safe to re-run.
-- Run AFTER migrations: psql -U swiftward -d swiftward -f seed.sql
--
-- API keys are inserted as 'PLACEHOLDER' and injected from env vars
-- by the swiftward-seed compose service.

-- ============================================================
-- Tenant: ensure 'default' tenant exists
-- ============================================================

INSERT INTO tenants (code, name, status)
VALUES ('default', 'Default', 'active')
ON CONFLICT (code) DO NOTHING;

-- ============================================================
-- LLM Provider: OpenAI
-- ============================================================

INSERT INTO ai_providers (id, tenant_id, code, name, provider_type, base_url, risk_class,
    timeout_ms, stream_timeout_ms, is_enabled)
VALUES (
    'a1000000-0000-0000-0000-000000000001',
    (SELECT id FROM tenants WHERE code = 'default'),
    'openai',
    'OpenAI',
    'openai',
    'https://api.openai.com/v1',
    'foreign',
    300000,   -- 5 min request timeout (agents do long tool-calling loops)
    600000,   -- 10 min stream timeout
    true
)
ON CONFLICT (id) DO UPDATE SET
    code              = EXCLUDED.code,
    name              = EXCLUDED.name,
    provider_type     = EXCLUDED.provider_type,
    base_url          = EXCLUDED.base_url,
    timeout_ms        = EXCLUDED.timeout_ms,
    stream_timeout_ms = EXCLUDED.stream_timeout_ms,
    is_enabled        = EXCLUDED.is_enabled;

-- ============================================================
-- Provider Key: OpenAI (PLACEHOLDER - real key injected by seed-api-keys)
-- ============================================================

INSERT INTO ai_provider_keys (id, provider_id, name, encrypted_key, priority, is_enabled)
VALUES (
    'a2000000-0000-0000-0000-000000000001',
    'a1000000-0000-0000-0000-000000000001',
    'default-openai',
    'PLACEHOLDER',
    0,
    true
)
ON CONFLICT (id) DO UPDATE SET
    name       = EXCLUDED.name,
    priority   = EXCLUDED.priority,
    is_enabled = EXCLUDED.is_enabled;
    -- Note: encrypted_key NOT updated on conflict to preserve real keys

-- ============================================================
-- Models
-- ============================================================

INSERT INTO ai_provider_models (id, provider_id, model_code, display_name, mode,
    input_cost_per_1m, output_cost_per_1m, context_window, max_output_tokens,
    supports_vision, supports_tools, supports_parallel_tools, supports_streaming)
VALUES
    ('a3000000-0000-0000-0000-000000000001',
     'a1000000-0000-0000-0000-000000000001',
     'gpt-4o', 'GPT-4o', 'completion',
     2.50, 10.00, 128000, 16384,
     true, true, true, true),

    ('a3000000-0000-0000-0000-000000000002',
     'a1000000-0000-0000-0000-000000000001',
     'gpt-5.4', 'GPT-5.4', 'completion',
     2.50, 10.00, 128000, 16384,
     true, true, true, true)
ON CONFLICT (id) DO UPDATE SET
    model_code            = EXCLUDED.model_code,
    display_name          = EXCLUDED.display_name,
    input_cost_per_1m     = EXCLUDED.input_cost_per_1m,
    output_cost_per_1m    = EXCLUDED.output_cost_per_1m,
    context_window        = EXCLUDED.context_window,
    max_output_tokens     = EXCLUDED.max_output_tokens,
    supports_vision       = EXCLUDED.supports_vision,
    supports_tools        = EXCLUDED.supports_tools,
    supports_parallel_tools = EXCLUDED.supports_parallel_tools,
    supports_streaming    = EXCLUDED.supports_streaming;
