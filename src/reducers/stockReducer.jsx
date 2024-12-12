import { createSlice } from "@reduxjs/toolkit";

const stockSlice = createSlice({
    name: 'stock',
    initialState: {
        data: {},
        loading: false,
        error: null,
    },
    reducers: {
        fetchStockStart: (state) => {
            state.loading = true;
            state.error = null;
        },
        fetchStockSuccess: (state, action) => {
            state.data = action.payload;
            state.loading = false;
        },
        fetchStockFailure: (state, action) => {
            state.error = action.payload;
            state.loading = false;
        }
    }
})

export const { fetchStockStart, fetchStockSuccess, fetchStockFailure} = stockSlice.actions;
export default stockSlice.reducer;