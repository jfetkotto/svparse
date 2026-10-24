module top (input signed [7:0] s, input [7:0] u, input int i, input logic [3:0] n, input bit flag,
            output logic o, output logic [7:0] sum);
  assign o = s < 8'sd5;
  assign o = u < 8'd5;
  assign o = s < 5;
  assign o = u < 5;
  assign o = $signed(u) < s;
  assign o = s < $signed({1'b0, u});
  assign o = i > 0;
  assign sum = s + flag;
  assign sum = u + u[0];
  assign o = (s > 0) == flag;
  assign o = n == 4'd3;
endmodule
