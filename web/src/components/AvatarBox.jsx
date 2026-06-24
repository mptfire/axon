import * as React from "react";
import { Avatar, Box, styled } from "@mui/material";
import { NavLink } from "react-router-dom";
import logo from "../img/ntfy-filled.svg";
import routes from "./routes";

const AvatarBoxContainer = styled(Box)`
  display: flex;
  flex-grow: 1;
  justify-content: center;
  flex-direction: column;
  align-content: center;
  align-items: center;
  height: 100dvh;
  max-width: min(400px, 90dvw);
  margin: auto;
`;
const AvatarBox = (props) => {
  const avatar = <Avatar sx={{ m: 2, width: 64, height: 64, borderRadius: 3 }} src={logo} variant="rounded" />;
  return (
    <AvatarBoxContainer>
      {/* The logo links back to the app, unless login is forced (no app to go back to without signing in) */}
      {config.require_login ? avatar : <NavLink to={routes.app}>{avatar}</NavLink>}
      {props.children}
    </AvatarBoxContainer>
  );
};

export default AvatarBox;
