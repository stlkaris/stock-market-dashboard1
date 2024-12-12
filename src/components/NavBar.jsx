import React from 'react'
import { Link } from 'react-router-dom'

const NavBar = () => {
  return (
    <nav className='bg-gray-800 text-white p-4'>
        <div className='container mx-auto flex justify-between items-center'>
            <h1 className='text-lg font-bold'>
                <Link to="/">Stock Market Dashboard</Link>
            </h1>
           <div>
            <Link className='px-4' to="/dashboard">Dashboard</Link>
            <Link className='px-4' to="/login">Login</Link>
           </div>
        </div>
    </nav>
  )
}

export default NavBar
