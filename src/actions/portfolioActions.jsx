export const addStockPortfolio = (stock) => (dispatch) => {
dispatch({type: 'portfolio/addStock', payload: stock });
}

export const removeStockFromPortfolio = (symbol) => (dispatch) => {
    dispatch({type: 'portfolio/removeStock', payload: symbol });
}