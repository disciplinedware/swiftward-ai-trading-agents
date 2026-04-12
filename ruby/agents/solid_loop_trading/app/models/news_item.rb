class NewsItem < ApplicationRecord
  scope :per_day, -> { group("DATE(published_at)").count }
  scope :per_month, -> { group("DATE_TRUNC('month', published_at)").count }
end
