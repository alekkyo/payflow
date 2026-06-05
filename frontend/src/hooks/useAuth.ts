// Re-export from context so all call sites share one state instance.
// Previously this file held its own useState, which meant Navbar and Login
// each had independent state — a login in Login wouldn't update Navbar.
export { useAuth } from '../context/AuthContext'
