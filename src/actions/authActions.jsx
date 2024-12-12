import { loginSucess, loginFailure, logout } from "../reducers/authReducer";
import { login as loginService } from '../services/authService';

export const loginUser = (email, password) => async (dispatch) => {
    try {
        const user = await loginService(email, password);
        dispatch(loginSucess(user));
    } catch (error) {
        dispatch(loginFailure(error.message));
    }
}

export const logoutUser = () => (dispatch) => {
    dispatch(logout());
};