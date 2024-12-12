import React from "react";
import ReactDom from 'react-dom';
import { BrowserRouter as Router } from 'react-router-dom'
import { Provider } from "react-redux";
import App from "./App";
import store from './store'
import './index.css';

const root = ReactDom.createRoot(document.getElementById('root'));
root.render(
    <Provider store={store}>
        <Router>
    <App />
    </Router>
    </Provider>
);
