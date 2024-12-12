import { configureStore } from "@reduxjs/toolkit";
import portfolioReducer from './reducers/portfolioReducer';
import stockReducer from './reducers/stockReducer';
import authReducer from './reducers/authReducer';

const store = configureStore({
 reducer: {
    portfolio: portfolioReducer,
    stocks: stockReducer,
    auth: authReducer,
 },
});

export default store;