use tokio_util::sync::CancellationToken;
use tracing::{error, info};

use ai_trading_agents::agents::simple_llm::{LlmAgentService, SessionDoneError};
use ai_trading_agents::config::{self, ROLE_LLM_AGENT};

#[tokio::main]
async fn main() -> std::process::ExitCode {
    std::process::ExitCode::from(run().await as u8)
}

async fn run() -> i32 {
    // Load config from env vars
    let cfg = config::Config::load();

    // Init tracing (structured logging)
    init_tracing(&cfg.logging);

    // Parse roles
    let roles = match config::read_roles(&cfg) {
        Ok(r) => r,
        Err(e) => {
            error!(error = %e, "Failed to parse roles");
            return 1;
        }
    };

    info!(?roles, "Starting AI Trading Agents Platform (Rust)");

    // Cancellation token (like context.Context in Go)
    let cancel = CancellationToken::new();
    let cancel_clone = cancel.clone();

    // Signal handler (Ctrl+C)
    tokio::spawn(async move {
        tokio::signal::ctrl_c()
            .await
            .expect("failed to listen for ctrl+c");
        info!("Received shutdown signal");
        cancel_clone.cancel();
    });

    // Dispatch by role
    if roles.contains(&ROLE_LLM_AGENT.to_string()) {
        let mut svc = LlmAgentService::new(cfg.llm_agent.clone(), cancel);

        if let Err(e) = svc.initialize() {
            error!(error = %e, "Failed to initialize LLM agent");
            return 1;
        }

        match svc.start().await {
            Ok(()) => {
                info!("LLM agent stopped");
                0
            }
            Err(e) => {
                // SessionDoneError is not an error - graceful exit from once mode
                if e.downcast_ref::<SessionDoneError>().is_some() {
                    info!("Session complete");
                    0
                } else {
                    error!(error = %e, "LLM agent failed");
                    1
                }
            }
        }
    } else {
        error!(?roles, "No supported role found (Rust binary only supports llm_agent)");
        1
    }
}

fn init_tracing(logging: &config::LoggingConfig) {
    use tracing_subscriber::EnvFilter;

    let filter = EnvFilter::try_from_default_env()
        .unwrap_or_else(|_| EnvFilter::new(&logging.level));

    if logging.format == "json" {
        tracing_subscriber::fmt()
            .json()
            .with_env_filter(filter)
            .init();
    } else {
        tracing_subscriber::fmt()
            .with_env_filter(filter)
            .init();
    }
}
