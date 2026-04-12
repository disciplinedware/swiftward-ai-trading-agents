Rails.application.routes.draw do
  resources :trading_sessions, only: [ :index, :show ]
  resources :agent_model_providers
  resources :agent_models
  resources :agent_runs do
    collection do
      post :recalculate_all
    end
    member do
      post :pause
      post :resume
      post :stop_agent
      post :resume_agent
      get :download
      get :mcp
      get :tools
      post :run_mcp_tool
    end
    resources :messages, only: [ :create, :show, :edit, :update ] do
      member do
        post :regenerate
        post :fork
      end
    end
  end

  resources :trading_scenarios
  resources :agent_batches do
    member do
      post :start
      post :stop
      post :restart_errors
      post :warm_up
      post :recalculate_stats
      post :stop_orchestrator
      post :resume_orchestrator
      get :download
    end
  end

  resources :backtesting_data, only: [] do
    collection do
      get :dashboard
      post :download_candles
      post :parse_news
    end
  end

  resources :agent_events, only: [ :index, :show ]

  post "mcp", to: "mcp#call"

  root "agent_runs#index"

  get "up" => "rails/health#show", as: :rails_health_check

  mount GoodJob::Engine => "good_job"
end
