import React from 'react'

const PerformanceIndicator = ({data}) => {
  return (
    <div>
      <h2 className='text-lg font-bold'>Performace Indicators</h2>
      <p>RSI: {data.rsi}</p>
      <p>MACD: {data.macd}</p>
    </div>
  )
}

export default PerformanceIndicator
