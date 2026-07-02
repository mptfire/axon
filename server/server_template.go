package server

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"text/template"

	"gopkg.in/yaml.v2"
	"heckel.io/ntfy/v2/model"
	"heckel.io/ntfy/v2/util"
	"heckel.io/ntfy/v2/util/sprig"
)

func (s *Server) handleBodyAsTemplatedTextMessage(m *model.Message, template templateMode, body *util.PeekedReadCloser, priorityStr string) error {
	body, err := util.Peek(body, max(s.config.MessageSizeLimit, jsonBodyBytesLimit))
	if err != nil {
		return err
	} else if body.LimitReached {
		return errHTTPEntityTooLargeJSONBody
	}
	peekedBody := strings.TrimSpace(string(body.PeekedBytes))
	if template.FileMode() {
		if err := s.renderTemplateFromFile(m, template.FileName(), peekedBody); err != nil {
			return err
		}
	} else {
		if err := s.renderTemplateFromParams(m, peekedBody, priorityStr); err != nil {
			return err
		}
	}
	if len(m.Title) > s.config.MessageSizeLimit || len(m.Message) > s.config.MessageSizeLimit {
		return errHTTPBadRequestTemplateMessageTooLarge
	}
	return nil
}

// renderTemplateFromFile transforms the JSON message body according to a template from the filesystem.
// The template file must be in the templates directory, or in the configured template directory.
func (s *Server) renderTemplateFromFile(m *model.Message, templateName, peekedBody string) error {
	if !templateNameRegex.MatchString(templateName) {
		return errHTTPBadRequestTemplateFileNotFound
	}
	templateContent, _ := templatesFs.ReadFile(filepath.Join(templatesDir, templateName+templateFileExtension)) // Read from the embedded filesystem first
	if s.config.TemplateDir != "" {
		if b, _ := os.ReadFile(filepath.Join(s.config.TemplateDir, templateName+templateFileExtension)); len(b) > 0 {
			templateContent = b
		}
	}
	if len(templateContent) == 0 {
		return errHTTPBadRequestTemplateFileNotFound
	}
	var tpl templateFile
	if err := yaml.Unmarshal(templateContent, &tpl); err != nil {
		return errHTTPBadRequestTemplateFileInvalid
	}
	var err error
	if tpl.Message != nil {
		if m.Message, err = s.renderTemplate(templateName+" (message)", *tpl.Message, peekedBody); err != nil {
			return err
		}
	}
	if tpl.Title != nil {
		if m.Title, err = s.renderTemplate(templateName+" (title)", *tpl.Title, peekedBody); err != nil {
			return err
		}
	}
	if tpl.Priority != nil {
		renderedPriority, err := s.renderTemplate(templateName+" (priority)", *tpl.Priority, peekedBody)
		if err != nil {
			return err
		}
		if m.Priority, err = util.ParsePriority(renderedPriority); err != nil {
			return errHTTPBadRequestPriorityInvalid
		}
	}
	return nil
}

// renderTemplateFromParams transforms the JSON message body according to the inline template in the
// message, title, and priority parameters.
func (s *Server) renderTemplateFromParams(m *model.Message, peekedBody string, priorityStr string) error {
	var err error
	if m.Message, err = s.renderTemplate("priority query parameter", m.Message, peekedBody); err != nil {
		return err
	}
	if m.Title, err = s.renderTemplate("title query parameter", m.Title, peekedBody); err != nil {
		return err
	}
	if priorityStr != "" {
		renderedPriority, err := s.renderTemplate("priority query parameter", priorityStr, peekedBody)
		if err != nil {
			return err
		}
		if m.Priority, err = util.ParsePriority(renderedPriority); err != nil {
			return errHTTPBadRequestPriorityInvalid
		}
	}
	return nil
}

// renderTemplate renders a template with the given JSON source data.
func (s *Server) renderTemplate(name, tpl, source string) (string, error) {
	if templateDisallowedRegex.MatchString(tpl) {
		return "", errHTTPBadRequestTemplateDisallowedFunctionCalls
	}
	var data any
	if err := json.Unmarshal([]byte(source), &data); err != nil {
		return "", errHTTPBadRequestTemplateMessageNotJSON
	}
	t, err := template.New("").Funcs(sprig.TxtFuncMap()).Parse(tpl)
	if err != nil {
		return "", errHTTPBadRequestTemplateInvalid.Wrap("%s", err.Error())
	}
	var buf bytes.Buffer
	limitWriter := util.NewLimitWriter(util.NewTimeoutWriter(&buf, templateMaxExecutionTime), util.NewFixedLimiter(templateMaxOutputBytes))
	if err := t.Execute(limitWriter, data); err != nil {
		return "", errHTTPBadRequestTemplateExecuteFailed.Wrap("template %s: %s", name, err.Error())
	}
	return strings.TrimSpace(strings.ReplaceAll(buf.String(), "\\n", "\n")), nil // replace any remaining "\n" (those outside of template curly braces) with newlines
}
