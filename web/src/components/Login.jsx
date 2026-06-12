import * as React from "react";
import { useState } from "react";
import {
  Typography,
  TextField,
  Button,
  Box,
  IconButton,
  InputAdornment,
  Dialog,
  DialogTitle,
  DialogContent,
  DialogContentText,
  DialogActions,
} from "@mui/material";
import WarningAmberIcon from "@mui/icons-material/WarningAmber";
import { NavLink } from "react-router-dom";
import { useTranslation } from "react-i18next";
import { Visibility, VisibilityOff } from "@mui/icons-material";
import accountApi from "../app/AccountApi";
import AvatarBox from "./AvatarBox";
import session from "../app/Session";
import routes from "./routes";
import { UnauthorizedError } from "../app/errors";

const Login = () => {
  const { t } = useTranslation();
  const [error, setError] = useState("");
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [showPassword, setShowPassword] = useState(false);
  const [resetOpen, setResetOpen] = useState(false);

  const handleSubmit = async (event) => {
    event.preventDefault();
    const user = { username, password };
    try {
      const token = await accountApi.login(user);
      console.log(`[Login] User auth for user ${user.username} successful, token is ${token}`);
      await session.store(user.username, token);
      window.location.href = routes.app;
    } catch (e) {
      console.log(`[Login] User auth for user ${user.username} failed`, e);
      if (e instanceof UnauthorizedError) {
        setError(t("Login failed: Invalid username or password"));
      } else {
        setError(e.message);
      }
    }
  };
  if (!config.enable_login) {
    return (
      <AvatarBox>
        <Typography sx={{ typography: "h6" }}>{t("login_disabled")}</Typography>
      </AvatarBox>
    );
  }
  return (
    <AvatarBox>
      <Typography sx={{ typography: "h6" }}>{t("login_title")}</Typography>
      <Box component="form" onSubmit={handleSubmit} noValidate sx={{ mt: 1 }}>
        <TextField
          margin="dense"
          required
          fullWidth
          id="username"
          label={t("signup_form_username")}
          name="username"
          value={username}
          onChange={(ev) => setUsername(ev.target.value.trim())}
          autoFocus
        />
        <TextField
          margin="dense"
          required
          fullWidth
          name="password"
          label={t("signup_form_password")}
          type={showPassword ? "text" : "password"}
          id="password"
          value={password}
          onChange={(ev) => setPassword(ev.target.value.trim())}
          autoComplete="current-password"
          InputProps={{
            endAdornment: (
              <InputAdornment position="end">
                <IconButton
                  aria-label={t("signup_form_toggle_password_visibility")}
                  onClick={() => setShowPassword(!showPassword)}
                  onMouseDown={(ev) => ev.preventDefault()}
                  edge="end"
                >
                  {showPassword ? <VisibilityOff /> : <Visibility />}
                </IconButton>
              </InputAdornment>
            ),
          }}
        />
        <Button type="submit" fullWidth variant="contained" disabled={username === "" || password === ""} sx={{ mt: 2, mb: 2 }}>
          {t("login_form_button_submit")}
        </Button>
        {error && (
          <Box
            sx={{
              mb: 1,
              display: "flex",
              flexGrow: 1,
              justifyContent: "center",
            }}
          >
            <WarningAmberIcon color="error" sx={{ mr: 1 }} />
            <Typography sx={{ color: "error.main" }}>{error}</Typography>
          </Box>
        )}
        <Box sx={{ width: "100%" }}>
          {config.enable_reset_password && (
            <div style={{ float: "left" }}>
              <Button variant="text" onClick={() => setResetOpen(true)} sx={{ textTransform: "none", p: 0, minWidth: 0 }}>
                {t("login_link_forgot_password")}
              </Button>
            </div>
          )}
          {config.enable_signup && (
            <div style={{ float: "right" }}>
              <NavLink to={routes.signup} variant="body1">
                {t("login_link_signup")}
              </NavLink>
            </div>
          )}
        </Box>
      </Box>
      <ForgotPasswordDialog open={resetOpen} onClose={() => setResetOpen(false)} />
    </AvatarBox>
  );
};

// ForgotPasswordDialog collects a username/email and asks the server to email a reset link. The
// response is uniform, so the dialog always shows the same "if an account exists" confirmation.
const ForgotPasswordDialog = (props) => {
  const { t } = useTranslation();
  const [identifier, setIdentifier] = useState("");
  const [sending, setSending] = useState(false);
  const [sent, setSent] = useState(false);

  const handleSubmit = async () => {
    try {
      setSending(true);
      await accountApi.requestPasswordReset(identifier);
    } catch (e) {
      console.log(`[Login] Password reset request failed`, e);
    } finally {
      setSending(false);
      setSent(true); // Uniform outcome regardless of success/failure (enumeration-safe)
    }
  };

  return (
    <Dialog open={props.open} onClose={props.onClose}>
      <DialogTitle>{t("login_reset_dialog_title")}</DialogTitle>
      <DialogContent>
        {sent ? (
          <DialogContentText>{t("login_reset_dialog_sent")}</DialogContentText>
        ) : (
          <>
            <DialogContentText>{t("login_reset_dialog_description")}</DialogContentText>
            <TextField
              autoFocus
              margin="dense"
              label={t("login_reset_dialog_identifier_label")}
              type="text"
              value={identifier}
              onChange={(ev) => setIdentifier(ev.target.value.trim())}
              fullWidth
              variant="standard"
            />
          </>
        )}
      </DialogContent>
      <DialogActions>
        {sent ? (
          <Button onClick={props.onClose}>{t("common_close")}</Button>
        ) : (
          <>
            <Button onClick={props.onClose}>{t("common_cancel")}</Button>
            <Button onClick={handleSubmit} disabled={sending || identifier === ""}>
              {t("login_reset_dialog_button_submit")}
            </Button>
          </>
        )}
      </DialogActions>
    </Dialog>
  );
};

export default Login;
