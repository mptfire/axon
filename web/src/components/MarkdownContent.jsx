import * as React from "react";
import { useEffect } from "react";
import { useRemark } from "react-remark";
import styled from "@emotion/styled";

const MarkdownContainer = styled("div")`
  line-height: 1;

  h1,
  h2,
  h3,
  h4,
  h5,
  h6,
  p,
  pre,
  ul,
  ol,
  blockquote {
    margin: 0;
  }

  p {
    line-height: 1.5;
  }

  blockquote,
  pre {
    border-radius: 3px;
    background: ${(props) => (props.theme.palette.mode === "light" ? "#f5f5f5" : "#333")};
  }

  pre {
    overflow-x: scroll;
    padding: 0.9rem;
  }

  ul,
  ol,
  blockquote {
    padding-inline: 1rem;
  }

  img {
    max-width: 100%;
  }
`;

// Renders text/markdown notification bodies. react-remark (and its remark/unified
// dependency stack) is heavy, so this component lives in its own module and is loaded
// lazily by Notifications.jsx -- it only ships when a markdown message is actually shown.
const MarkdownContent = ({ content }) => {
  const [reactContent, setMarkdownSource] = useRemark();

  useEffect(() => {
    setMarkdownSource(content);
  }, [content]);

  return <MarkdownContainer>{reactContent}</MarkdownContainer>;
};

export default MarkdownContent;
