export const fetchNews = async (symbol) => {
  const response = await fetch(`https://api.example.com/news/${symbol}`)
  const data = await response.json()
  return data.articles
}