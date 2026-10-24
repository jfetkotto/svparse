module top #(parameter W = 8, parameter DEPTH = 16) (input clk, input [7:0] narrow, input [15:0] wide,
                                                     output logic [7:0] q, output logic [3:0] r, output wire [7:0] w, output logic [3:0] idx);
  logic [7:0] count;
  logic [W-1:0] pw;
  always_ff @(posedge clk) begin
    count <= count + 1;          // an unsized literal only needs one bit
    q <= narrow;
    q <= wide[7:0];
    q <= wide >> 8;
    r <= narrow[3:0];
    r <= 4'd15;
    r <= 255 & 4'hF;
    q <= 32'd200;                // the value fits
    q <= '1;
    q <= DEPTH;                  // a parameter whose value fits
    idx <= $clog2(DEPTH);
    pw <= count;
    q <= (count == 0) ? narrow : count;
  end
  assign w = narrow;
  assign w = {4'b0, r};
  logic [7:0] init = 8'hFF;
endmodule
