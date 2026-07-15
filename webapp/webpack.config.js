const path = require("path");
const PLUGIN_ID = require("../plugin.json").id;

const NPM_TARGET = process.env.npm_lifecycle_event;
let mode = "production";
let devtool = false;
if (NPM_TARGET === "debug" || NPM_TARGET === "debug:watch") {
  mode = "development";
  devtool = "source-map";
}

module.exports = {
  entry: ["./src/index.tsx"],
  resolve: {
    modules: ["src", "node_modules", path.resolve(__dirname)],
    extensions: ["*", ".js", ".jsx", ".ts", ".tsx"]
  },
  module: {
    rules: [
      {
        test: /\.(js|jsx|ts|tsx)$/,
        exclude: /node_modules/,
        use: {
          loader: "babel-loader",
          options: { cacheDirectory: true }
        }
      },
      {
        test: /\.css$/,
        use: ["style-loader", "css-loader"]
      }
    ]
  },
  // Mattermost가 전역으로 제공하는 라이브러리는 externals 처리.
  //
  externals: [
    {
      react: "React",
      redux: "Redux",
      "react-redux": "ReactRedux",
      "prop-types": "PropTypes",
      "react-bootstrap": "ReactBootstrap",
      "react-router-dom": "ReactRouterDom"
    },
    function ({ request }, callback) {
      // React 18 의 createRoot 용 react-dom/client 는 번들에 포함
      if (request === "react-dom/client") {
        return callback();
      }
      // 나머지 react-dom import 는 전역 ReactDOM 사용
      if (request === "react-dom") {
        return callback(null, "ReactDOM");
      }
      callback();
    }
  ],
  output: {
    devtoolNamespace: PLUGIN_ID,
    path: path.join(__dirname, "/dist"),
    publicPath: "/",
    filename: "main.js"
    // ⚠️ libraryTarget 을 절대 설정하지 말 것 —
    //    설정하면 IIFE 가 module.exports 로 wrap 되어
    //    window.registerPlugin() 호출이 실행되지 않습니다.
    //    그 결과: "Unable to generate plugin webapp bundle" 에러.
  },
  devtool,
  mode
};
