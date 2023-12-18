package api

import (
	"github.com/gin-gonic/gin"
)

// StartServer starts the Gin server on the specified port.
func StartServer(port string) {
	router := gin.Default()

	// Set up routes
	setupRoutes(router)

	// Start the server
	router.Run(":" + port)
}

// setupRoutes defines the routes for the server.
func setupRoutes(router *gin.Engine) {
	router.POST("/update", handleUpdate)
}

// handleUpdate handles the POST request to update a sensor value.
func handleUpdate(c *gin.Context) {
	// Implement the logic to handle the update.
	// You will need to extract the key/value pair from the request and process it.
	c.JSON(200, gin.H{
		"message": "Update received",
	})
}
