import { useMemo } from 'react'
import { useMarkets, usePrices } from '@/hooks/use-market'
import { useAllFunding, useAllOpenInterest } from '@/hooks/use-market-data'
import { useLatestNews } from '@/hooks/use-news'
import { MarketPricesTable } from '@/components/market/MarketPricesTable'
import { FundingTable } from '@/components/market/FundingTable'
import { NewsFeed } from '@/components/market/NewsFeed'

// Fallback when list_markets fails (e.g. Binance exchangeInfo timeout).
const DEFAULT_MARKETS = ['ETH-USDC', 'BTC-USDC', 'ETH-USDT', 'BTC-USDT']

export function Market() {
  const { data: marketsData } = useMarkets()

  const marketNames = useMemo(
    () => marketsData?.markets?.map((m) => m.pair) ?? DEFAULT_MARKETS,
    [marketsData],
  )

  const { data: pricesData } = usePrices(marketNames)
  const { fundingMap } = useAllFunding(marketNames)
  const { oiMap } = useAllOpenInterest(marketNames)
  const { data: newsData } = useLatestNews(30)

  return (
    <div>
      <h1 className="text-xl font-semibold text-text-primary mb-6">Market</h1>

      <div className="space-y-6">
        <MarketPricesTable
          prices={pricesData?.prices ?? []}
          source={pricesData?.source ?? '-'}
        />

        <FundingTable
          markets={marketNames}
          fundingMap={fundingMap}
          oiMap={oiMap}
        />

        <NewsFeed articles={newsData?.articles ?? []} />
      </div>
    </div>
  )
}
