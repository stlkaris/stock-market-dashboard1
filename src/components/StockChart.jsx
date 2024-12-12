import React, {useState, useEffect} from 'react'
import { Line } from 'recharts';
import { fetchHistoricalData } from '../services/stockService';

const StockChart = ({ symbol }) => {
    const [data, setData] = useState(null);

    useEffect(() => {
        const LoadData = async () => {
            try {
                const historicalData = await fetchHistoricalData(symbol);
                setData({
                  labels: historicalData.map((point) => point.date),
                  datasets: [
                    {
                        label: `${symbol} Prices`,
                        data: historicalData.map((point) => point.close),
                        borderColor: 'blue',
                        fill: false,
                    }
                  ]
                })
            } catch (error) {
                console.error("Error loading stock data:", error);
            }
        }

    }, [symbol]);
    if(!data) return <p>Loading chart...</p>;

  return <Line data={data} />
}

export default StockChart
