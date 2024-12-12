import {createSlice} from '@reduxjs/toolkit'


const authSlice = createSlice({
    name: 'auth',
    initialState: {
        user: null,
         isAuthenticated: false,
         error: null,
    },
    reducers: {
        loginSucess: (state, action) => {
            state.user = action.payload;
            state.isAuthenticated = true;
        },
        loginFailure: (state, action) => {
            state.error = action.payload;
            state.isAuthenticated = false;
        },
        logout: (state) => {
            state.error = null;
            state.isAuthenticated = false;
        }
    }
})

export const {loginSucess, loginFailure, logout} = authSlice.actions;
export default authSlice.reducer;