# This file is auto-generated from the current state of the database. Instead
# of editing this file, please use the migrations feature of Active Record to
# incrementally modify your database, and then regenerate this schema definition.
#
# This file is the source Rails uses to define your schema when running `bin/rails
# db:schema:load`. When creating a new database, `bin/rails db:schema:load` tends to
# be faster and is potentially less error prone than running all of your
# migrations from scratch. Old migrations may fail to apply correctly if those
# migrations use external dependencies or application code.
#
# It's strongly recommended that you check this file into your version control system.

ActiveRecord::Schema[8.1].define(version: 2026_04_11_120000) do
  # These are extensions that must be enabled in order to support this database
  enable_extension "pg_catalog.plpgsql"

  create_table "agent_batches", force: :cascade do |t|
    t.string "agent_mode", default: "backtesting", null: false
    t.bigint "agent_model_id", null: false
    t.integer "attempts"
    t.text "conclusion"
    t.integer "concurrency"
    t.datetime "created_at", null: false
    t.datetime "end_at"
    t.string "hardware"
    t.string "prefix"
    t.datetime "start_at"
    t.string "status"
    t.integer "step_interval"
    t.datetime "updated_at", null: false
    t.integer "wakeup_interval_minutes", default: 15, null: false
    t.datetime "warm_up_at"
    t.text "warm_up_response"
    t.integer "warm_up_status", default: 0
    t.index ["agent_model_id"], name: "index_agent_batches_on_agent_model_id"
  end

  create_table "agent_model_providers", force: :cascade do |t|
    t.string "api_key", null: false
    t.string "base_url", null: false
    t.datetime "created_at", null: false
    t.text "description"
    t.string "name", null: false
    t.datetime "updated_at", null: false
  end

  create_table "agent_models", force: :cascade do |t|
    t.bigint "agent_model_provider_id"
    t.datetime "created_at", null: false
    t.jsonb "data"
    t.text "description"
    t.string "llm_model_name"
    t.string "name"
    t.decimal "price_per_cached_token", precision: 20, scale: 15, default: "0.0"
    t.decimal "price_per_input_token", precision: 20, scale: 15, default: "0.0"
    t.decimal "price_per_output_token", precision: 20, scale: 15, default: "0.0"
    t.jsonb "tools"
    t.datetime "updated_at", null: false
    t.index ["agent_model_provider_id"], name: "index_agent_models_on_agent_model_provider_id"
  end

  create_table "agent_runs", force: :cascade do |t|
    t.bigint "agent_batch_id"
    t.string "agent_id"
    t.bigint "agent_model_id", null: false
    t.datetime "created_at", null: false
    t.string "filename"
    t.datetime "last_active_at"
    t.string "orchestrator_status", default: "initial", null: false
    t.decimal "score"
    t.string "status"
    t.text "system_prompt"
    t.string "title"
    t.bigint "trading_scenario_id"
    t.datetime "updated_at", null: false
    t.text "user_prompt"
    t.integer "wakeup_interval_minutes", default: 15, null: false
    t.index ["agent_batch_id"], name: "index_agent_runs_on_agent_batch_id"
    t.index ["agent_model_id"], name: "index_agent_runs_on_agent_model_id"
    t.index ["trading_scenario_id"], name: "index_agent_runs_on_trading_scenario_id"
  end

  create_table "good_job_batches", id: :uuid, default: -> { "gen_random_uuid()" }, force: :cascade do |t|
    t.integer "callback_priority"
    t.text "callback_queue_name"
    t.datetime "created_at", null: false
    t.text "description"
    t.datetime "discarded_at"
    t.datetime "enqueued_at"
    t.datetime "finished_at"
    t.datetime "jobs_finished_at"
    t.text "on_discard"
    t.text "on_finish"
    t.text "on_success"
    t.jsonb "serialized_properties"
    t.datetime "updated_at", null: false
  end

  create_table "good_job_executions", id: :uuid, default: -> { "gen_random_uuid()" }, force: :cascade do |t|
    t.uuid "active_job_id", null: false
    t.datetime "created_at", null: false
    t.interval "duration"
    t.text "error"
    t.text "error_backtrace", array: true
    t.integer "error_event", limit: 2
    t.datetime "finished_at"
    t.text "job_class"
    t.uuid "process_id"
    t.text "queue_name"
    t.datetime "scheduled_at"
    t.jsonb "serialized_params"
    t.datetime "updated_at", null: false
    t.index ["active_job_id", "created_at"], name: "index_good_job_executions_on_active_job_id_and_created_at"
    t.index ["process_id", "created_at"], name: "index_good_job_executions_on_process_id_and_created_at"
  end

  create_table "good_job_processes", id: :uuid, default: -> { "gen_random_uuid()" }, force: :cascade do |t|
    t.datetime "created_at", null: false
    t.integer "lock_type", limit: 2
    t.jsonb "state"
    t.datetime "updated_at", null: false
  end

  create_table "good_job_settings", id: :uuid, default: -> { "gen_random_uuid()" }, force: :cascade do |t|
    t.datetime "created_at", null: false
    t.text "key"
    t.datetime "updated_at", null: false
    t.jsonb "value"
    t.index ["key"], name: "index_good_job_settings_on_key", unique: true
  end

  create_table "good_jobs", id: :uuid, default: -> { "gen_random_uuid()" }, force: :cascade do |t|
    t.uuid "active_job_id"
    t.uuid "batch_callback_id"
    t.uuid "batch_id"
    t.text "concurrency_key"
    t.datetime "created_at", null: false
    t.datetime "cron_at"
    t.text "cron_key"
    t.text "error"
    t.integer "error_event", limit: 2
    t.integer "executions_count"
    t.datetime "finished_at"
    t.boolean "is_discrete"
    t.text "job_class"
    t.text "labels", array: true
    t.datetime "locked_at"
    t.uuid "locked_by_id"
    t.datetime "performed_at"
    t.integer "priority"
    t.text "queue_name"
    t.uuid "retried_good_job_id"
    t.datetime "scheduled_at"
    t.jsonb "serialized_params"
    t.datetime "updated_at", null: false
    t.index ["active_job_id", "created_at"], name: "index_good_jobs_on_active_job_id_and_created_at"
    t.index ["batch_callback_id"], name: "index_good_jobs_on_batch_callback_id", where: "(batch_callback_id IS NOT NULL)"
    t.index ["batch_id"], name: "index_good_jobs_on_batch_id", where: "(batch_id IS NOT NULL)"
    t.index ["concurrency_key", "created_at"], name: "index_good_jobs_on_concurrency_key_and_created_at"
    t.index ["concurrency_key"], name: "index_good_jobs_on_concurrency_key_when_unfinished", where: "(finished_at IS NULL)"
    t.index ["cron_key", "created_at"], name: "index_good_jobs_on_cron_key_and_created_at_cond", where: "(cron_key IS NOT NULL)"
    t.index ["cron_key", "cron_at"], name: "index_good_jobs_on_cron_key_and_cron_at_cond", unique: true, where: "(cron_key IS NOT NULL)"
    t.index ["finished_at"], name: "index_good_jobs_jobs_on_finished_at_only", where: "(finished_at IS NOT NULL)"
    t.index ["job_class"], name: "index_good_jobs_on_job_class"
    t.index ["labels"], name: "index_good_jobs_on_labels", where: "(labels IS NOT NULL)", using: :gin
    t.index ["locked_by_id"], name: "index_good_jobs_on_locked_by_id", where: "(locked_by_id IS NOT NULL)"
    t.index ["priority", "created_at"], name: "index_good_job_jobs_for_candidate_lookup", where: "(finished_at IS NULL)"
    t.index ["priority", "created_at"], name: "index_good_jobs_jobs_on_priority_created_at_when_unfinished", order: { priority: "DESC NULLS LAST" }, where: "(finished_at IS NULL)"
    t.index ["priority", "scheduled_at"], name: "index_good_jobs_on_priority_scheduled_at_unfinished_unlocked", where: "((finished_at IS NULL) AND (locked_by_id IS NULL))"
    t.index ["queue_name", "scheduled_at"], name: "index_good_jobs_on_queue_name_and_scheduled_at", where: "(finished_at IS NULL)"
    t.index ["scheduled_at"], name: "index_good_jobs_on_scheduled_at", where: "(finished_at IS NULL)"
  end

  create_table "ledger_entries", force: :cascade do |t|
    t.decimal "amount", precision: 24, scale: 8
    t.string "asset"
    t.string "category"
    t.datetime "created_at", null: false
    t.bigint "trading_order_id"
    t.bigint "trading_session_id", null: false
    t.datetime "updated_at", null: false
    t.index ["trading_order_id"], name: "index_ledger_entries_on_trading_order_id"
    t.index ["trading_session_id"], name: "index_ledger_entries_on_trading_session_id"
  end

  create_table "news_items", force: :cascade do |t|
    t.text "content"
    t.datetime "created_at", null: false
    t.datetime "edited_at"
    t.datetime "published_at"
    t.string "symbol"
    t.string "telegram_channel"
    t.bigint "telegram_message_id"
    t.string "title"
    t.datetime "updated_at", null: false
    t.index ["published_at"], name: "index_news_items_on_published_at"
    t.index ["symbol"], name: "index_news_items_on_symbol"
    t.index ["telegram_channel", "telegram_message_id"], name: "idx_news_items_tg_unique", unique: true
  end

  create_table "ohlc_candles", force: :cascade do |t|
    t.decimal "close"
    t.datetime "created_at", null: false
    t.decimal "high"
    t.decimal "low"
    t.decimal "open"
    t.string "symbol"
    t.datetime "timestamp"
    t.datetime "updated_at", null: false
    t.decimal "volume"
    t.index ["symbol", "timestamp"], name: "idx_ohlc_candles_symbol_timestamp", unique: true
    t.index ["symbol"], name: "index_ohlc_candles_on_symbol"
    t.index ["timestamp"], name: "index_ohlc_candles_on_timestamp"
  end

  create_table "solid_loop_events", force: :cascade do |t|
    t.datetime "created_at", null: false
    t.float "duration", default: 0.0, null: false
    t.bigint "eventable_id", null: false
    t.string "eventable_type", null: false
    t.jsonb "headers", default: {}, null: false
    t.string "http_method", null: false
    t.text "log"
    t.bigint "loop_id"
    t.bigint "message_id"
    t.string "name", null: false
    t.jsonb "request_data", default: {}, null: false
    t.jsonb "response_data", default: {}, null: false
    t.string "tool_call_id"
    t.datetime "updated_at", null: false
    t.string "url"
    t.index ["eventable_type", "eventable_id"], name: "index_solid_loop_events_on_eventable"
    t.index ["loop_id"], name: "index_solid_loop_events_on_loop_id"
    t.index ["message_id"], name: "index_solid_loop_events_on_message_id"
    t.index ["tool_call_id"], name: "index_solid_loop_events_on_tool_call_id"
  end

  create_table "solid_loop_loops", force: :cascade do |t|
    t.string "agent_class_name"
    t.decimal "cost", precision: 15, scale: 10, default: "0.0", null: false
    t.datetime "created_at", null: false
    t.float "duration_generation", default: 0.0, null: false
    t.float "duration_parsing", default: 0.0, null: false
    t.float "duration_tools", default: 0.0, null: false
    t.float "duration_total", default: 0.0, null: false
    t.text "error_message"
    t.jsonb "state", default: {}, null: false
    t.string "status", default: "init", null: false
    t.integer "step_count", default: 0, null: false
    t.bigint "subject_id"
    t.string "subject_type"
    t.string "title"
    t.integer "tokens_completion", default: 0, null: false
    t.integer "tokens_prompt", default: 0, null: false
    t.integer "tokens_prompt_cached", default: 0, null: false
    t.integer "tokens_total", default: 0, null: false
    t.datetime "updated_at", null: false
    t.index ["status"], name: "index_solid_loop_loops_on_status"
    t.index ["subject_type", "subject_id"], name: "index_solid_loop_loops_on_subject"
  end

  create_table "solid_loop_mcp_sessions", force: :cascade do |t|
    t.datetime "created_at", null: false
    t.bigint "loop_id", null: false
    t.string "mcp_name", null: false
    t.jsonb "metadata", default: {}, null: false
    t.string "session_id", null: false
    t.datetime "updated_at", null: false
    t.index ["loop_id", "mcp_name"], name: "index_solid_loop_mcp_sessions_on_loop_id_and_mcp_name", unique: true
    t.index ["loop_id"], name: "index_solid_loop_mcp_sessions_on_loop_id"
  end

  create_table "solid_loop_mcp_tools", force: :cascade do |t|
    t.datetime "created_at", null: false
    t.text "description"
    t.jsonb "input_schema", default: {}, null: false
    t.bigint "mcp_session_id", null: false
    t.string "name", null: false
    t.datetime "updated_at", null: false
    t.index ["mcp_session_id", "name"], name: "index_solid_loop_mcp_tools_on_mcp_session_id_and_name", unique: true
    t.index ["mcp_session_id"], name: "index_solid_loop_mcp_tools_on_mcp_session_id"
  end

  create_table "solid_loop_messages", force: :cascade do |t|
    t.text "content"
    t.decimal "cost", precision: 15, scale: 10, default: "0.0", null: false
    t.datetime "created_at", null: false
    t.float "duration_generation", default: 0.0, null: false
    t.float "duration_thinking", default: 0.0, null: false
    t.text "error_message"
    t.boolean "is_hidden", default: false, null: false
    t.bigint "loop_id", null: false
    t.jsonb "metadata", default: {}, null: false
    t.text "reasoning_content"
    t.string "role", null: false
    t.string "status", null: false
    t.integer "tokens_completion", default: 0, null: false
    t.integer "tokens_prompt", default: 0, null: false
    t.integer "tokens_prompt_cached", default: 0, null: false
    t.integer "tokens_total", default: 0, null: false
    t.string "tool_call_id"
    t.jsonb "tool_calls_raw", default: [], null: false
    t.float "tps", default: 0.0, null: false
    t.float "ttft", default: 0.0, null: false
    t.datetime "updated_at", null: false
    t.index ["loop_id", "is_hidden"], name: "index_solid_loop_messages_on_loop_id_and_is_hidden"
    t.index ["loop_id"], name: "index_solid_loop_messages_on_loop_id"
    t.index ["role"], name: "index_solid_loop_messages_on_role"
    t.index ["tool_call_id"], name: "index_solid_loop_messages_on_tool_call_id"
  end

  create_table "solid_loop_tool_calls", force: :cascade do |t|
    t.jsonb "arguments", default: {}, null: false
    t.datetime "created_at", null: false
    t.float "duration", default: 0.0, null: false
    t.text "error_message"
    t.string "function_name", null: false
    t.boolean "is_success"
    t.bigint "mcp_session_id"
    t.bigint "message_id", null: false
    t.jsonb "metadata", default: {}, null: false
    t.text "result"
    t.string "tool_call_id", null: false
    t.datetime "updated_at", null: false
    t.index ["mcp_session_id"], name: "index_solid_loop_tool_calls_on_mcp_session_id"
    t.index ["message_id", "tool_call_id"], name: "index_solid_loop_tool_calls_on_message_id_and_tool_call_id", unique: true
    t.index ["message_id"], name: "index_solid_loop_tool_calls_on_message_id"
    t.index ["metadata"], name: "index_solid_loop_tool_calls_on_metadata", opclass: :jsonb_path_ops, using: :gin
  end

  create_table "trading_orders", force: :cascade do |t|
    t.decimal "amount"
    t.datetime "created_at", null: false
    t.decimal "equity_at_execution"
    t.datetime "executed_at"
    t.string "order_id"
    t.decimal "price"
    t.text "reasoning"
    t.string "side"
    t.string "status"
    t.string "symbol"
    t.bigint "trading_session_id", null: false
    t.datetime "updated_at", null: false
    t.index ["trading_session_id"], name: "index_trading_orders_on_trading_session_id"
  end

  create_table "trading_scenarios", force: :cascade do |t|
    t.string "category"
    t.datetime "created_at", null: false
    t.datetime "end_at"
    t.string "filename"
    t.decimal "initial_balance"
    t.string "name", null: false
    t.text "prompt", null: false
    t.datetime "start_at"
    t.integer "step_interval"
    t.string "trading_pair"
    t.datetime "updated_at", null: false
    t.text "user_prompt"
  end

  create_table "trading_sessions", force: :cascade do |t|
    t.datetime "created_at", null: false
    t.datetime "last_sync_at"
    t.jsonb "open_orders"
    t.jsonb "portfolio"
    t.integer "time_offset"
    t.datetime "updated_at", null: false
    t.string "uuid"
    t.string "workdir_uuid"
    t.index ["uuid"], name: "index_trading_sessions_on_uuid"
  end

  add_foreign_key "agent_batches", "agent_models"
  add_foreign_key "agent_models", "agent_model_providers"
  add_foreign_key "agent_runs", "agent_batches"
  add_foreign_key "agent_runs", "agent_models"
  add_foreign_key "agent_runs", "trading_scenarios"
  add_foreign_key "ledger_entries", "trading_orders"
  add_foreign_key "ledger_entries", "trading_sessions"
  add_foreign_key "solid_loop_mcp_sessions", "solid_loop_loops", column: "loop_id"
  add_foreign_key "solid_loop_mcp_tools", "solid_loop_mcp_sessions", column: "mcp_session_id"
  add_foreign_key "solid_loop_messages", "solid_loop_loops", column: "loop_id"
  add_foreign_key "solid_loop_tool_calls", "solid_loop_mcp_sessions", column: "mcp_session_id"
  add_foreign_key "solid_loop_tool_calls", "solid_loop_messages", column: "message_id"
  add_foreign_key "trading_orders", "trading_sessions"
end
