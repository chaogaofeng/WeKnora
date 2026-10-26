package knowledgeops

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

func (s *Service) registerFollowupHTTP(g *gin.RouterGroup) {
	g.POST("/runs/:id/followups", s.addFollowup)
}
func (s *Service) addFollowup(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 16384)
	var input FollowupInput
	if c.ShouldBindJSON(&input) != nil {
		reply(c, nil, ErrInvalid)
		return
	}
	value, err := s.AddFollowup(c.Request.Context(), tenant(c), c.Param("id"), input)
	reply(c, value, err)
}
