import React, {useState} from 'react'


const StockSearch = ({onSearch}) => {
    const [query, setQuery] = useState('');

    const handleSearch = (e) => {
        e.preventDefault();
        onSearch(query)
    }
  return (
    <form className='flex gap-2 mb-4' onSubmit={handleSearch}>
        <input 
        type="text" 
        placeholder='Search for a stock...'
        className='border p-2 w-full'
        value={query}
        onChange={(e) => setQuery(e.target.value)}
        />
    </form>
  )
}

export default StockSearch
