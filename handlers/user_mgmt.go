package handlers

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io/ioutil"
	"memphis-control-plane/broker"
	"memphis-control-plane/logger"
	"memphis-control-plane/models"
	"memphis-control-plane/utils"
	"mime/multipart"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/dgrijalva/jwt-go"
	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"golang.org/x/crypto/bcrypt"
)

type UserMgmtHandler struct{}

func isUserExist(username string) (bool, models.User, error) {
	filter := bson.M{"username": username}
	var user models.User
	err := usersCollection.FindOne(context.TODO(), filter).Decode(&user)
	if err == mongo.ErrNoDocuments {
		return false, user, nil
	} else if err != nil {
		return false, user, err
	}
	return true, user, nil
}

func isRootUserExist() (bool, error) {
	filter := bson.M{"user_type": "root"}
	var user models.User
	err := usersCollection.FindOne(context.TODO(), filter).Decode(&user)
	if err == mongo.ErrNoDocuments {
		return false, nil
	} else if err != nil {
		return false, err
	}
	return true, nil
}

func authenticateUser(username string, password string) (bool, models.User, error) {
	filter := bson.M{"username": username}
	var user models.User
	err := usersCollection.FindOne(context.TODO(), filter).Decode(&user)
	if err == mongo.ErrNoDocuments {
		return false, models.User{}, nil
	} else if err != nil {
		return false, models.User{}, err
	}

	hashedPwd := []byte(user.Password)
	err = bcrypt.CompareHashAndPassword(hashedPwd, []byte(password))
	if err != nil {
		return false, models.User{}, nil
	}

	return true, user, nil
}

func validateUserType(userType string) error {
	if userType != "application" && userType != "management" {
		return errors.New("user type has to be application/management")
	}
	return nil
}

// TODO check against hub api
func validateHubCreds(hubUsername string, hubPassword string) error {
	if hubUsername != "" && hubPassword != "" {
		// TODO
	}
	return nil
}

// TODO terminate all user connections
func updateUserResources(user models.User) error {
	if user.UserType == "application" {
		err := broker.RemoveUser(user.Username)
		if err != nil {
			return err
		}
	}

	_, err := tokensCollection.DeleteOne(context.TODO(), bson.M{"username": user.Username})
	if err != nil {
		return err
	}

	_, err = factoriesCollection.UpdateMany(context.TODO(),
		bson.M{"created_by_user": user.Username},
		bson.M{"$set": bson.M{"created_by_user": user.Username + "(deleted)"}},
	)
	if err != nil {
		return err
	}

	_, err = stationsCollection.UpdateMany(context.TODO(),
		bson.M{"created_by_user": user.Username},
		bson.M{"$set": bson.M{"created_by_user": user.Username + "(deleted)"}},
	)
	if err != nil {
		return err
	}

	return nil
}

func validateUsername(username string) error {
	re := regexp.MustCompile("^[a-z0-9_.]*$")

	validName := re.MatchString(username)
	if !validName {
		return errors.New("username has to include only letters/numbers/./_ ")
	}
	return nil
}

func createTokens(user models.User) (string, string, error) {
	atClaims := jwt.MapClaims{}
	atClaims["user_id"] = user.ID.Hex()
	atClaims["username"] = user.Username
	atClaims["user_type"] = user.UserType
	atClaims["creation_date"] = user.CreationDate
	atClaims["already_logged_in"] = user.AlreadyLoggedIn
	atClaims["avatar_id"] = user.AvatarId
	atClaims["exp"] = time.Now().Add(time.Minute * time.Duration(configuration.JWT_EXPIRES_IN_MINUTES)).Unix()
	at := jwt.NewWithClaims(jwt.SigningMethodHS256, atClaims)
	token, err := at.SignedString([]byte(configuration.JWT_SECRET))
	if err != nil {
		return "", "", err
	}

	atClaims["exp"] = time.Now().Add(time.Minute * time.Duration(configuration.REFRESH_JWT_EXPIRES_IN_MINUTES)).Unix()
	at = jwt.NewWithClaims(jwt.SigningMethodHS256, atClaims)
	refreshToken, err := at.SignedString([]byte(configuration.REFRESH_JWT_SECRET))
	if err != nil {
		return "", "", err
	}

	return token, refreshToken, nil
}

func imageToBase64(imagePath string) (string, error) {
	bytes, err := ioutil.ReadFile(imagePath)
	if err != nil {
		return "", err
	}

	fileExt := filepath.Ext(imagePath)
	var base64Encoding string

	switch fileExt {
	case ".jpeg":
		base64Encoding += "data:image/jpeg;base64,"
	case ".png":
		base64Encoding += "data:image/png;base64,"
	case ".jpg":
		base64Encoding += "data:image/jpg;base64,"
	}

	base64Encoding += base64.StdEncoding.EncodeToString(bytes)
	return base64Encoding, nil
}

func CreateRootUserOnFirstSystemLoad() error {
	exist, err := isRootUserExist()
	if err != nil {
		return err
	}

	if !exist {
		password := configuration.ROOT_PASSWORD

		hashedPwd, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.MinCost)
		if err != nil {
			return err
		}
		hashedPwdString := string(hashedPwd)

		newUser := models.User{
			ID:              primitive.NewObjectID(),
			Username:        "root",
			Password:        hashedPwdString,
			HubUsername:     "",
			HubPassword:     "",
			UserType:        "root",
			CreationDate:    time.Now(),
			AlreadyLoggedIn: false,
			AvatarId:        1,
		}

		_, err = usersCollection.InsertOne(context.TODO(), newUser)
		if err != nil {
			return err
		}
	}

	return nil
}

func (umh UserMgmtHandler) Login(c *gin.Context) {
	var body models.LoginSchema
	ok := utils.Validate(c, &body, false, nil)
	if !ok {
		return
	}

	username := strings.ToLower(body.Username)
	authenticated, user, err := authenticateUser(username, body.Password)
	if err != nil {
		logger.Error("Login error: " + err.Error())
		c.AbortWithStatusJSON(500, gin.H{"message": "Server error"})
		return
	}
	if !authenticated || user.UserType == "application" {
		c.AbortWithStatusJSON(401, gin.H{"message": "Unauthorized"})
		return
	}

	token, refreshToken, err := createTokens(user)
	if err != nil {
		logger.Error("Login error: " + err.Error())
		c.AbortWithStatusJSON(500, gin.H{"message": "Server error"})
		return
	}

	opts := options.Update().SetUpsert(true)
	_, err = tokensCollection.UpdateOne(context.TODO(),
		bson.M{"username": user.Username},
		bson.M{"$set": bson.M{"jwt_token": token, "refresh_token": refreshToken}},
		opts,
	)
	if err != nil {
		logger.Error("Login error: " + err.Error())
		c.AbortWithStatusJSON(500, gin.H{"message": "Server error"})
		return
	}

	if !user.AlreadyLoggedIn {
		usersCollection.UpdateOne(context.TODO(),
			bson.M{"_id": user.ID},
			bson.M{"$set": bson.M{"already_logged_in": true}},
		)
	}

	domain := ""
	secure := false
	c.SetCookie("jwt-refresh-token", refreshToken, configuration.REFRESH_JWT_EXPIRES_IN_MINUTES*60*1000, "/", domain, secure, true)
	c.IndentedJSON(200, gin.H{
		"jwt":               token,
		"expires_in":        configuration.JWT_EXPIRES_IN_MINUTES * 60 * 1000,
		"user_id":           user.ID,
		"username":          user.Username,
		"user_type":         user.UserType,
		"creation_date":     user.CreationDate,
		"already_logged_in": user.AlreadyLoggedIn,
		"avatar_id":         user.AvatarId,
	})
}

func (umh UserMgmtHandler) RefreshToken(c *gin.Context) {
	user := getUserDetailsFromMiddleware(c)
	token, refreshToken, err := createTokens(user)
	if err != nil {
		logger.Error("RefreshToken error: " + err.Error())
		c.AbortWithStatusJSON(500, gin.H{"message": "Server error"})
		return
	}

	opts := options.Update().SetUpsert(true)
	_, err = tokensCollection.UpdateOne(context.TODO(),
		bson.M{"username": user.Username},
		bson.M{"$set": bson.M{"jwt_token": token, "refresh_token": refreshToken}},
		opts,
	)
	if err != nil {
		logger.Error("RefreshToken error: " + err.Error())
		c.AbortWithStatusJSON(500, gin.H{"message": "Server error"})
		return
	}

	domain := ""
	secure := true
	c.SetCookie("jwt-refresh-token", refreshToken, configuration.REFRESH_JWT_EXPIRES_IN_MINUTES*60*1000, "/", domain, secure, true)
	c.IndentedJSON(200, gin.H{
		"jwt":               token,
		"expires_in":        configuration.JWT_EXPIRES_IN_MINUTES * 60 * 1000,
		"user_id":           user.ID,
		"username":          user.Username,
		"user_type":         user.UserType,
		"creation_date":     user.CreationDate,
		"already_logged_in": user.AlreadyLoggedIn,
		"avatar_id":         user.AvatarId,
	})
}

func (umh UserMgmtHandler) Logout(c *gin.Context) {
	user := getUserDetailsFromMiddleware(c)
	_, err := tokensCollection.DeleteOne(context.TODO(), bson.M{"username": user.Username})
	if err != nil {
		logger.Error("Logout error: " + err.Error())
		c.AbortWithStatusJSON(500, gin.H{"message": "Server error"})
		return
	}

	c.IndentedJSON(200, gin.H{})
}

// TODO
func (umh UserMgmtHandler) AuthenticateNatsUser(c *gin.Context) {
	publicKey := c.Param("publicKey")
	if publicKey != "" {
		fmt.Println(publicKey)
		// authenticated, user, err := authenticateUser(body.Username, body.Password)
		// if err != nil {
		// 	logger.Error("AuthenticateNats error: " + err.Error())
		// 	c.AbortWithStatusJSON(500, gin.H{"message": "Server error"})
		// 	return
		// }

		// if !authenticated || user.UserType == "management" {
		// 	c.AbortWithStatusJSON(401, gin.H{"message": "Unauthorized"})
		// 	return
		// }
	}

	c.IndentedJSON(200, gin.H{})
}

func (umh UserMgmtHandler) AddUser(c *gin.Context) {
	var body models.AddUserSchema
	ok := utils.Validate(c, &body, false, nil)
	if !ok {
		return
	}

	username := strings.ToLower(body.Username)
	exist, _, err := isUserExist(username)
	if err != nil {
		logger.Error("CreateUser error: " + err.Error())
		c.AbortWithStatusJSON(500, gin.H{"message": "Server error"})
		return
	}
	if exist {
		c.AbortWithStatusJSON(400, gin.H{"message": "A user with this username is already exist"})
		return
	}

	userType := strings.ToLower(body.UserType)
	userTypeError := validateUserType(userType)
	if userTypeError != nil {
		c.AbortWithStatusJSON(400, gin.H{"message": userTypeError.Error()})
		return
	}

	usernameError := validateUsername(username)
	if usernameError != nil {
		c.AbortWithStatusJSON(400, gin.H{"message": usernameError.Error()})
		return
	}

	var hashedPwdString string
	var avatarId int
	if userType == "management" {
		if body.Password == "" {
			c.AbortWithStatusJSON(400, gin.H{"message": "Password was not provided"})
			return
		}

		hashedPwd, err := bcrypt.GenerateFromPassword([]byte(body.Password), bcrypt.MinCost)
		if err != nil {
			logger.Error("CreateUser error: " + err.Error())
			c.AbortWithStatusJSON(500, gin.H{"message": "Server error"})
			return
		}
		hashedPwdString = string(hashedPwd)

		avatarId = 1
		if body.AvatarId > 0 {
			avatarId = body.AvatarId
		}
	}

	err = validateHubCreds(body.HubUsername, body.HubPassword)
	if err != nil {
		logger.Error("CreateUser error: " + err.Error())
		c.AbortWithStatusJSON(500, gin.H{"message": err.Error()})
		return
	}

	var brokerConnectionCreds string
	if userType == "application" {
		brokerConnectionCreds, err = broker.AddUser(username)
		if err != nil {
			logger.Error("CreateUser error: " + err.Error())
			c.AbortWithStatusJSON(500, gin.H{"message": err.Error()})
			return
		}
	}

	newUser := models.User{
		ID:              primitive.NewObjectID(),
		Username:        username,
		Password:        hashedPwdString,
		HubUsername:     body.HubUsername,
		HubPassword:     body.HubPassword,
		UserType:        userType,
		CreationDate:    time.Now(),
		AlreadyLoggedIn: false,
		AvatarId:        avatarId,
	}

	_, err = usersCollection.InsertOne(context.TODO(), newUser)
	if err != nil {
		logger.Error("CreateUser error: " + err.Error())
		c.AbortWithStatusJSON(500, gin.H{"message": "Server error"})
		return
	}

	c.IndentedJSON(200, gin.H{
		"id":                    newUser.ID,
		"username":              username,
		"hub_username":          body.HubUsername,
		"hub_password":          body.HubPassword,
		"user_type":             userType,
		"creation_date":         newUser.CreationDate,
		"already_logged_in":     false,
		"avatar_id":             body.AvatarId,
		"broker_connection_creds": brokerConnectionCreds,
	})
}

func (umh UserMgmtHandler) GetAllUsers(c *gin.Context) {
	type filteredUser struct {
		ID              primitive.ObjectID `json:"id" bson:"_id"`
		Username        string             `json:"username" bson:"username"`
		UserType        string             `json:"user_type" bson:"user_type"`
		CreationDate    time.Time          `json:"creation_date" bson:"creation_date"`
		AlreadyLoggedIn bool               `json:"already_logged_in" bson:"already_logged_in"`
		AvatarId        int                `json:"avatar_id" bson:"avatar_id"`
	}
	var users []filteredUser

	cursor, err := usersCollection.Find(context.TODO(), bson.M{})
	if err != nil {
		logger.Error("GetAllUsers error: " + err.Error())
		c.AbortWithStatusJSON(500, gin.H{"message": "Server error"})
		return
	}

	if err = cursor.All(context.TODO(), &users); err != nil {
		logger.Error("GetAllUsers error: " + err.Error())
		c.AbortWithStatusJSON(500, gin.H{"message": "Server error"})
		return
	}

	if len(users) == 0 {
		c.IndentedJSON(200, []models.User{})
	} else {
		c.IndentedJSON(200, users)
	}
}

func (umh UserMgmtHandler) RemoveUser(c *gin.Context) {
	var body models.RemoveUserSchema
	ok := utils.Validate(c, &body, false, nil)
	if !ok {
		return
	}

	username := strings.ToLower(body.Username)
	user := getUserDetailsFromMiddleware(c)
	if user.Username == username {
		c.AbortWithStatusJSON(400, gin.H{"message": "You can't remove your own user"})
		return
	}

	exist, userToRemove, err := isUserExist(username)
	if err != nil {
		logger.Error("RemoveUser error: " + err.Error())
		c.AbortWithStatusJSON(500, gin.H{"message": "Server error"})
		return
	}
	if !exist {
		c.AbortWithStatusJSON(400, gin.H{"message": "User does not exist"})
		return
	}
	if userToRemove.UserType == "root" {
		c.AbortWithStatusJSON(400, gin.H{"message": "You can not remove the root user"})
		return
	}

	_, err = usersCollection.DeleteOne(context.TODO(), bson.M{"username": username})
	if err != nil {
		logger.Error("RemoveUser error: " + err.Error())
		c.AbortWithStatusJSON(500, gin.H{"message": "Server error"})
		return
	}

	err = updateUserResources(userToRemove)
	if err != nil {
		logger.Error("RemoveUser error: " + err.Error())
		c.AbortWithStatusJSON(500, gin.H{"message": err.Error()})
		return
	}

	c.IndentedJSON(200, gin.H{})
}

func (umh UserMgmtHandler) RemoveMyUser(c *gin.Context) {
	user := getUserDetailsFromMiddleware(c)

	if user.UserType == "root" {
		c.AbortWithStatusJSON(500, gin.H{"message": "Root user can not be deleted"})
		return
	}

	_, err := usersCollection.DeleteOne(context.TODO(), bson.M{"username": user.Username})
	if err != nil {
		logger.Error("RemoveMyUser error: " + err.Error())
		c.AbortWithStatusJSON(500, gin.H{"message": "Server error"})
		return
	}

	err = updateUserResources(user)
	if err != nil {
		logger.Error("RemoveMyUser error: " + err.Error())
		c.AbortWithStatusJSON(500, gin.H{"message": err.Error()})
		return
	}

	c.IndentedJSON(200, gin.H{})
}

func (umh UserMgmtHandler) EditHubCreds(c *gin.Context) {
	var body models.EditHubCredsSchema
	ok := utils.Validate(c, &body, false, nil)
	if !ok {
		return
	}

	err := validateHubCreds(body.HubUsername, body.HubPassword)
	if err != nil {
		logger.Error("EditHubCreds error: " + err.Error())
		c.AbortWithStatusJSON(500, gin.H{"message": err.Error()})
		return
	}

	user := getUserDetailsFromMiddleware(c)
	_, err = usersCollection.UpdateOne(context.TODO(),
		bson.M{"username": user.Username},
		bson.M{"$set": bson.M{"hub_username": body.HubUsername, "hub_password": body.HubPassword}},
	)
	if err != nil {
		logger.Error("EditHubCreds error: " + err.Error())
		c.AbortWithStatusJSON(500, gin.H{"message": "Server error"})
		return
	}

	c.IndentedJSON(200, gin.H{
		"id":                user.ID,
		"username":          user.Username,
		"hub_username":      body.HubUsername,
		"hub_password":      body.HubPassword,
		"user_type":         user.UserType,
		"creation_date":     user.CreationDate,
		"already_logged_in": user.AlreadyLoggedIn,
		"avatar_id":         user.AvatarId,
	})
}

func (umh UserMgmtHandler) EditAvatar(c *gin.Context) {
	var body models.EditAvatarSchema
	ok := utils.Validate(c, &body, false, nil)
	if !ok {
		return
	}

	avatarId := 1
	if body.AvatarId > 0 {
		avatarId = body.AvatarId
	}

	user := getUserDetailsFromMiddleware(c)
	_, err := usersCollection.UpdateOne(context.TODO(),
		bson.M{"username": user.Username},
		bson.M{"$set": bson.M{"avatar_id": avatarId}},
	)
	if err != nil {
		logger.Error("EditAvatar error: " + err.Error())
		c.AbortWithStatusJSON(500, gin.H{"message": "Server error"})
		return
	}

	c.IndentedJSON(200, gin.H{
		"id":                user.ID,
		"username":          user.Username,
		"hub_username":      user.HubUsername,
		"hub_password":      user.HubPassword,
		"user_type":         user.UserType,
		"creation_date":     user.CreationDate,
		"already_logged_in": user.AlreadyLoggedIn,
		"avatar_id":         avatarId,
	})
}

func (umh UserMgmtHandler) EditCompanyLogo(c *gin.Context) {
	var file multipart.FileHeader
	ok := utils.Validate(c, nil, true, &file)
	if !ok {
		return
	}

	fileName := "company_logo" + filepath.Ext(file.Filename)
	if err := c.SaveUploadedFile(&file, fileName); err != nil {
		logger.Error("EditCompanyLogo error: " + err.Error())
		c.AbortWithStatusJSON(500, gin.H{"message": "Server error"})
		return
	}

	base64Encoding, err := imageToBase64(fileName)
	if err != nil {
		logger.Error("EditCompanyLogo error: " + err.Error())
		c.AbortWithStatusJSON(500, gin.H{"message": "Server error"})
		return
	}

	_ = os.Remove(fileName)

	newImage := models.Image{
		ID:    primitive.NewObjectID(),
		Name:  "company_logo",
		Image: base64Encoding,
	}

	_, err = imagesCollection.InsertOne(context.TODO(), newImage)
	if err != nil {
		logger.Error("EditCompanyLogo error: " + err.Error())
		c.AbortWithStatusJSON(500, gin.H{"message": "Server error"})
		return
	}

	c.IndentedJSON(200, gin.H{"image": base64Encoding})
}

func (umh UserMgmtHandler) RemoveCompanyLogo(c *gin.Context) {
	_, err := imagesCollection.DeleteOne(context.TODO(), bson.M{"name": "company_logo"})
	if err != nil {
		logger.Error("RemoveCompanyLogo error: " + err.Error())
		c.AbortWithStatusJSON(500, gin.H{"message": "Server error"})
		return
	}

	c.IndentedJSON(200, gin.H{})
}

func (umh UserMgmtHandler) GetCompanyLogo(c *gin.Context) {
	var image models.Image
	err := imagesCollection.FindOne(context.TODO(), bson.M{"name": "company_logo"}).Decode(&image)
	if err == mongo.ErrNoDocuments {
		c.IndentedJSON(200, gin.H{"image": ""})
		return
	} else if err != nil {
		logger.Error("GetCompanyLogo error: " + err.Error())
		c.AbortWithStatusJSON(500, gin.H{"message": "Server error"})
		return
	}

	c.IndentedJSON(200, gin.H{"image": image.Image})
}
