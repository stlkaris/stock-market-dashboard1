import React, {useState, useEffect} from "react";
import {fetchNews} from '../services/newsService';


const NewsFeed = ({symbol}) => {
    const [news, setNews] =useState([]);


useEffect (() => {
 const loadNews = async () => {
    const newsData = await fetchNews(symbol);
    setNews(newsData)
 }
 if (symbol) loadNews();
}, [symbol]);

return (
  <div className="news-feed">
    <h2 className="text-lg font-bold">News Feed</h2>
    {news.length ? (
    <ul>
        {news.map((article, index)=> (
        <li key={index} className="mb-4">
            <a href={article.url} target="_blank" rel="noopener noreferrer" className="text-blue-600">
            {article.title}
            </a>
        </li>
        ))}
    </ul>
    ) : (
        <p>No news available</p>
    )}
  </div>
)
}

export default NewsFeed;