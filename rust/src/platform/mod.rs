use std::sync::Arc;

use tokio_util::sync::CancellationToken;

use crate::config::Config;

/// Shared context for all services - mirrors Go's platform.ServiceContext.
#[derive(Clone)]
pub struct ServiceContext {
    pub config: Arc<Config>,
    pub cancel: CancellationToken,
    pub roles: Vec<String>,
}

impl ServiceContext {
    pub fn new(config: Config, cancel: CancellationToken, roles: Vec<String>) -> Self {
        Self {
            config: Arc::new(config),
            cancel,
            roles,
        }
    }
}
