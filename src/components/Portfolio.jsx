import React from 'react'

const Portfolio = ({portfolio}) => {
  const dispatch = useDispatch();
  const portfolio = useSelector((state) => state.portfolio.stocks);

  const handleAddStock = () => {
    const newStock = { symbol: 'AAPL', shares: 10, price: 150};
    dispatch(addStockToPortfolio(newStock));
  }
  return (
    <div>
      <h2 className='text-lg font-bold'>Your Portfolio</h2>
      <ul>
        {portfolio.map((stock) => (
        <li key={stock.symbol}>
            {stock.symbol}: {stock.shares} shares @ ${stock.price}
        </li>
        ))}
      </ul>
      <button
        onClick={handleAddStock}
         className="mt-4 px-4 py-2 bg-blue-500 text-white rounded hover:bg-blue-600"
         >
          Add Stock
         </button>
    </div>
  )
}

export default Portfolio
