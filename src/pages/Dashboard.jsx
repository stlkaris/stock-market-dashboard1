import React, {useState, useEffect}from 'react'
import { useSelector} from 'react-redux'
import StockChart from '../components/StockChart';
import NewsFeed from '../components/NewsFeed'
import axios from 'axios';

const Dashboard = () => {
    const [selectedStock, setSelectedStock] = useState('AAPL');
    const  [newsArticles, setNewsArticle] = useState([])
    const [stockData, setStockData] = useState([])
    const portfolio = useSelector((state) => state.portfolio.stocks);
    
    const fetchStockData = async (symbol) => {
      try {
        const response = await axios.get(
          `https://api.twelvedata.com/time_series?symbol=${symbol}&interval=1min&api=API_KEY`
        )
      } catch (error) {
        
      }
    }
    return (
    <div className='container mx-auto'>
      <h1 className='text-2xl font-bold'>Dashboard</h1>
      <div className='mt-4'>
      <StockChart symbol={selectedStock}/>
      </div>
      <div className='mt-4'>
      <NewsFeed symbol={selectedStock} />
      </div>
      <div className='mt-4'>
        <h2 className='text-lg font-bold'> Your Portfolio</h2>
        <ul>
          {portfolio.map((stock) => (
          <li key={stock.symbol}>
            {stock.symbol}: {stock.shares} shares @ ${stock.price}
          </li>
          ))}
        </ul>
      </div>
    </div>
  )
}

export default Dashboard
