package knowledgeops

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

func (s *Service) registerAdoptionHTTP(g *gin.RouterGroup) {
	g.GET("/adoption-targets", s.getAdoptionTargets)
	g.POST("/runs/:id/adoptions", s.adoptAnswer)
	g.POST("/runs/:id/adoptions/:adoptionID/retry", s.retryAdoption)
}
func (s *Service) getAdoptionTargets(c *gin.Context) {
	items, err := s.AdoptionTargets(c.Request.Context(), tenant(c), c.Query("target_type"))
	reply(c, gin.H{"items": items}, err)
}
func (s *Service) adoptAnswer(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 192000)
	var input AdoptionInput
	if c.ShouldBindJSON(&input) != nil {
		reply(c, nil, ErrInvalid)
		return
	}
	value, err := s.Adopt(c.Request.Context(), tenant(c), c.Param("id"), input)
	reply(c, value, err)
}
func (s *Service) retryAdoption(c *gin.Context) {
	value, err := s.RetryAdoption(c.Request.Context(), tenant(c), c.Param("id"), c.Param("adoptionID"))
	reply(c, value, err)
}
