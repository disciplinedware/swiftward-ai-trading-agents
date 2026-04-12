class BacktestingDataController < ApplicationController
  def dashboard
    @news_count = NewsItem.count
    @news_last = NewsItem.order(published_at: :desc).first
    @news_per_day = NewsItem.group("DATE(published_at)").count.transform_keys { |k| k.to_s }.sort_by { |k, _v| k }.last(30).to_h
    @news_per_month = NewsItem.group("DATE_TRUNC('month', published_at)").count.transform_keys { |k| k.to_s }.sort.to_h

    @candles_count = OhlcCandle.count
    @candles_first = OhlcCandle.order(:timestamp).first
    @candles_last = OhlcCandle.order(:timestamp).last
    @available_tickers = OhlcCandle.select(:symbol).distinct.order(:symbol).map(&:symbol)

    @download_candles_running = GoodJob::Job.where(job_class: [ "DownloadCandlesJob", "BinanceOhlcDataFetcherJob" ]).where(finished_at: nil).exists?
    @parse_news_running = GoodJob::Job.where(job_class: "ParseNewsJob").where(finished_at: nil).exists?

    news_dir = Rails.root.join("storage", "external", "news")
    @news_files = Dir.glob(news_dir.join("**", "*.json")).reject { |f| File.basename(f).start_with?("._") || f.include?("__MACOSX") }

    @candles_job_status = job_status_text([ "DownloadCandlesJob", "BinanceOhlcDataFetcherJob" ])
    @news_job_status = job_status_text("ParseNewsJob")
    @candles_job_last = GoodJob::Job.where(job_class: [ "DownloadCandlesJob", "BinanceOhlcDataFetcherJob" ]).order(created_at: :desc).first
    @news_job_last = GoodJob::Job.where(job_class: "ParseNewsJob").order(created_at: :desc).first
  end

  def download_candles
    DownloadCandlesJob.perform_later
    redirect_to dashboard_backtesting_data_path, notice: "Download candles job enqueued."
  end

  def parse_news
    ParseNewsJob.perform_later
    redirect_to dashboard_backtesting_data_path, notice: "Parse news job enqueued."
  end

  private

  def job_status_text(job_classes)
    job_classes = Array(job_classes)
    job = GoodJob::Job.where(job_class: job_classes).order(created_at: :desc).first
    return "Never run" unless job

    if job.finished_at.present?
      if job.error.present?
        "Failed"
      else
        "Completed"
      end
    else
      "Running..."
    end
  end

  COLUMNS = %w[symbol timestamp open high low close volume].freeze
end
