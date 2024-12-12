import { fetchStockStart, fetchStockSuccess, fetchStockFailure} from '../reducers/stockReducer'
import { fetchHistoricalData } from '../services/stockService';

export const fetchStockData = (symbol) => async (dispatch) => {
    dispatch(fetchStockStart());
    try {
        const data =await fetchHistoricalData(symbol);
        dispatch(fetchStockSuccess(data));
    } catch (error) {
        dispatch(fetchStockFailure(error.message))
    }
};