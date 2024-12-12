import {getAuth, signInWithEmailAndPassword} from 'firebase/auth';

const auth = getAuth();

export const login = async (email, password) => {
    try {
        const userCredential = await signInWithEmailAndPassword(auth, email, password);
        return userCredential.user;
    } catch (error) {
        console.error("Error logging in:", error);
        throw error;
        
    }

}