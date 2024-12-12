import { createSlice } from "@reduxjs/toolkit";

const portfolioSlice = createSlice ({
    name: 'portfolio',
    initialState: {
        stocks: [],
    },
    reducers: {
        addStock: (state, action) => {
            state.stocks = state.stocks(action.payload)

        },
        removeStock: (state, action) => {
            state.stocks = state.stocks.filter((stock) => stock.symbol !== action.payload)
        },
        updateStock: (state, action) => {
            state.stocks = state.stocks.finderIndex((stock) => stock.symbol === action.payload.symbol)
            if(index !== -1) {
                state.stocks[index] = action.payload
            }
        }
    }
})

export const {addStock, removeStock, updateStock} = portfolioSlice.actions;
export default portfolioSlice.reducer;