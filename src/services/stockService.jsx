export const fetchHistoricalData = async (symbol) => {
  const response = await fetch('https://api.example.com/historical/${symbol}')
  const data = await response.json();
  return data.prices
}