import React from 'react'
import StockSearch from '../components/StockSearch';


const Home = () => {
    const handleSearch = (query) => {
      console.log(`Searching for: ${query}`);
    }
  return (
    <div className='container mx-auto'>
      <h1 className='text-2xl font-bold text-center my-4'>Welcome to the Stock Market Dashboard</h1>
      <StockSearch onSearch={handleSearch} />
    </div>
  )
}

export default Home
