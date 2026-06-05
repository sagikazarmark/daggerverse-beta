{ pkgs, ... }:

{
  dotenv.enable = true;

  dagger.enable = true;
  env.DAGGER_X_RELEASE = "v1.0.0-beta.3";

  languages = {
    dang = {
      package = (
        pkgs.dang.overrideAttrs (old: {
          src = pkgs.fetchFromGitHub {
            owner = "vito";
            repo = "dang";
            rev = "0a11c1e282de2d3c1bb842c96a637c3fc6ba6f4e";
            hash = "sha256-dIRbz+Z5581Ysv6C9nEcmAZG8f4bHev1uV/pbhGK8BU=";
          };
        })
      );
    };
  };
}
