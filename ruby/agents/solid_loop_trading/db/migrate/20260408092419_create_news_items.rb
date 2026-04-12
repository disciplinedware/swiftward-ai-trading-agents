class CreateNewsItems < ActiveRecord::Migration[8.1]
  def change
    create_table :news_items do |t|
      t.string :title
      t.text :content
      t.string :symbol
      t.datetime :published_at
      t.datetime :edited_at
      t.string :telegram_channel
      t.bigint :telegram_message_id

      t.timestamps
    end

    add_index :news_items, :published_at
    add_index :news_items, :symbol
    add_index :news_items, [:telegram_channel, :telegram_message_id], unique: true, name: "idx_news_items_tg_unique"
  end
end
